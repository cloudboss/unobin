package runtime

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/encoding/ub"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/stateref"
)

// ErrInstanceGone is returned by ensureCompositeScope when a per-
// instance composite scope is requested for a key that the boundary's
// `@for-each` iterable no longer yields. Plan-time seeding of prior
// state treats this as a signal to skip rather than fail; orphan
// destroy steps for the missing instance still emit through the
// usual orphan path.
var ErrInstanceGone = errors.New("instance no longer in iterable")

// DefaultParallelism is the in-flight cap apply uses when no explicit
// value is given on the Executor or in the plan file.
const DefaultParallelism = 10

// Executor owns the factory graph, libraries, inputs, and state backend.
// PlanV2 computes a saved plan, ApplyPlanV2 executes its reviewed decisions,
// and RefreshV2 records current resource observations.
type Executor struct {
	DAG            *DAG
	Libraries      map[string]*Library
	LibraryCatalog *LibraryCatalog
	Inputs         map[string]any

	AssetCatalog   *asset.Catalog
	AssetCache     *asset.Cache
	RootAssetSetID string

	// SyntaxSource is the typed factory body for grammar-first callers.
	SyntaxSource *syntax.FactoryBody

	Store   state.Backend
	Factory state.FactoryInfo

	// PlanBackend describes the state provider recorded in a version 2 plan.
	PlanBackend *StateRefV2

	// Parallelism caps the number of in-flight resource, data-source, and
	// action steps during ApplyPlanV2. Zero or negative uses the saved
	// plan's limit, or DefaultParallelism when no limit was recorded.
	Parallelism int

	// Destroy makes PlanV2 remove recorded entries using their saved
	// bindings and configurations, without evaluating root outputs.
	Destroy bool

	// Drain, when non-nil, lets the caller ask the scheduler to stop
	// dispatching new steps without canceling the apply context. The
	// runner closes this channel on SIGINT so in-flight CRUD calls
	// finish and their state writes commit; SIGTERM cancels the
	// context directly. A nil channel disables the drain signal.
	Drain <-chan struct{}

	// Events, when non-nil, receives one ApplyEvent per step stage
	// during ApplyPlanV2: start when the scheduler hands the step to a
	// worker, done or fail when the worker returns. The caller owns
	// the channel and is responsible for sizing the buffer and
	// closing it after ApplyPlanV2 returns. A nil channel disables
	// event emission.
	Events chan<- ApplyEvent

	// internalConfigurations holds the decoded value of each internal
	// configuration once its node evaluates, keyed by node address.
	// Apply evaluates configurations on worker goroutines while
	// consumers read concurrently, so access goes through internalMu.
	internalConfigurations map[string]any
	internalMu             sync.Mutex
}

// storeInternalConfiguration records the decoded value of an internal
// configuration under its node address.
func (e *Executor) storeInternalConfiguration(addr string, value any) {
	e.internalMu.Lock()
	defer e.internalMu.Unlock()
	if e.internalConfigurations == nil {
		e.internalConfigurations = map[string]any{}
	}
	e.internalConfigurations[addr] = value
}

// internalConfiguration returns the decoded value of an internal
// configuration, or false when its node has not evaluated.
func (e *Executor) internalConfiguration(addr string) (any, bool) {
	e.internalMu.Lock()
	defer e.internalMu.Unlock()
	v, ok := e.internalConfigurations[addr]
	return v, ok
}

// effectiveParallelism returns the in-flight cap apply should honor.
func (e *Executor) effectiveParallelism() int {
	if e.Parallelism > 0 {
		return e.Parallelism
	}
	return DefaultParallelism
}

// configFor returns the decoded config to pass to a CRUD call on the given
// node. A configured alias reads from its evaluated library-config node; an
// unconfigured alias uses nil for libraries without config or a decoded empty
// config for empty config schemas.
func (e *Executor) configFor(n *Node) any {
	if e.DAG != nil {
		if addr, ok := libraryConfigNode(e.DAG.Nodes, n.Composite, n.Alias); ok {
			v, _ := e.internalConfiguration(addr)
			return v
		}
	}
	return emptyDecodedConfig(e.librariesFor(n)[n.Alias])
}

func emptyDecodedConfig(lib *Library) any {
	if lib == nil || lib.Configuration == nil || !lib.Configuration.Empty() {
		return nil
	}
	decoded, err := decodeLibraryConfig(lib, map[string]any{})
	if err != nil {
		return nil
	}
	return decoded
}

// ExecResult contains evaluated outputs, action and data values, and
// the revision written by apply.
type ExecResult struct {
	Outputs    map[string]any
	Actions    map[string]any
	Data       map[string]any
	WrittenRev string
}

type runState struct {
	eval    *EvalContext
	outputs map[string]any

	partialEvaluation   bool
	initializeComposite func(string, *EvalContext) error

	// order is the DAG's topological order, computed once per run.
	// Plan's walk and per-instance composite expansion both follow it.
	order []string

	// composites holds one EvalContext per composite call site. Lazily
	// built when a node inside a composite first needs evaluation. Inputs
	// in each scope are the call site args; Resources, Data, Actions
	// hold sibling outputs as the internals complete.
	composites map[string]*EvalContext

	// forEachInstances memoizes each `@for-each` node's evaluated
	// iterable by template address, so sibling instances share one
	// evaluation per run. Sound because the iterable's references are
	// dependencies of every instance: their values settle before the
	// first instance needs them and cannot change within the run.
	forEachInstances map[string]map[string]any
}

func (e *Executor) newEvaluationRunState(inputs map[string]any) (*runState, error) {
	order, err := e.DAG.TopologicalOrder()
	if err != nil {
		return nil, err
	}
	rootAssets, err := e.rootAssetSet()
	if err != nil {
		return nil, err
	}
	rs := &runState{
		eval: &EvalContext{
			Inputs:     inputs,
			Resources:  make(map[string]any),
			Data:       make(map[string]any),
			Actions:    make(map[string]any),
			Libraries:  e.Libraries,
			Assets:     rootAssets,
			AssetCache: e.AssetCache,
			locals:     e.rootLocalScope(),
		},
		order:            order,
		outputs:          make(map[string]any),
		composites:       make(map[string]*EvalContext),
		forEachInstances: make(map[string]map[string]any),
	}
	return rs, nil
}

func (e *Executor) rootAssetSet() (*asset.Set, error) {
	return e.assetSet(e.RootAssetSetID)
}

func (e *Executor) assetSet(id string) (*asset.Set, error) {
	if id == "" {
		return nil, nil
	}
	if e.AssetCatalog == nil {
		return nil, fmt.Errorf("asset set %q: asset catalog is not configured", id)
	}
	set, ok := e.AssetCatalog.Set(id)
	if !ok {
		return nil, fmt.Errorf("asset set %q: not found in asset catalog", id)
	}
	return set, nil
}

// librariesFor returns the import table the runtime should resolve n's
// library alias against. Top-level nodes use the executor's root
// Libraries; composite-internal nodes use their boundary's Libraries so a
// composite stays self-contained. Falls back to e.Libraries when a
// composite has no Libraries populated, preserving backward
// compatibility for direct test construction.
func (e *Executor) librariesFor(n *Node) map[string]*Library {
	if n.Composite == "" {
		return e.Libraries
	}
	if boundary, ok := e.DAG.Nodes[n.Composite]; ok && boundary.Libraries != nil {
		return boundary.Libraries
	}
	return e.Libraries
}

// compositeBodyLibraries returns the import table the composite's own
// body (internals and outputs) should resolve aliases against. The
// boundary node carries the composite's Libraries; an unset table falls
// back to the executor's root for test compositions that don't set it.
func compositeBodyLibraries(boundary *Node, fallback map[string]*Library) map[string]*Library {
	if boundary.Libraries != nil {
		return boundary.Libraries
	}
	return fallback
}

// enclosingScope returns the scope enclosing a step or call-site
// address: the root scope for a top-level address, otherwise the
// composite scope named by everything before the last `/`. Deriving
// the parent from the address rather than from the node keeps
// `['key']` segments, so an address inside a `@for-each` composite
// resolves to its own instance's scope, not the template's.
func (e *Executor) enclosingScope(rs *runState, addr string) (*EvalContext, error) {
	parentAddr := DirectParent(addr)
	if parentAddr == "" {
		return rs.eval, nil
	}
	return e.ensureCompositeScope(rs, parentAddr)
}

func (e *Executor) ensureCompositeScope(rs *runState, callSite string) (*EvalContext, error) {
	if scope, ok := rs.composites[callSite]; ok {
		return scope, nil
	}
	boundary, ok := e.DAG.Nodes[templateAddress(callSite)]
	if !ok {
		return nil, fmt.Errorf("composite %s: boundary node not in DAG", callSite)
	}
	parent, err := e.enclosingScope(rs, callSite)
	if err != nil {
		return nil, diagnostic.Context(
			fmt.Sprintf("composite %s: build parent scope", callSite), err,
		)
	}
	setAddr, instKey, keyed := splitEntryKey(callSite)
	bodyScope := parent
	if keyed {
		instances, err := forEachInstancesFor(rs, setAddr, boundary.ForEach, parent)
		if err != nil {
			return nil, diagnostic.Context(
				fmt.Sprintf("composite %s: eval @for-each", callSite), err,
			)
		}
		value, ok := instances[instKey]
		if !ok {
			return nil, diagnostic.Context(fmt.Sprintf("composite %s", callSite), ErrInstanceGone)
		}
		bodyScope = childScopeWithEach(parent, instKey, value)
	}
	var args map[string]any
	if rs.partialEvaluation {
		args, _, err = planEvalBody(boundary.Body, bodyScope)
	} else {
		args, err = evalBody(boundary.Body, bodyScope)
	}
	if err != nil {
		return nil, diagnostic.Context(
			fmt.Sprintf("composite %s: eval call args", callSite), err,
		)
	}
	assets, err := e.assetSet(boundary.AssetSetID)
	if err != nil {
		return nil, diagnostic.Context(fmt.Sprintf("composite %s", callSite), err)
	}
	scope := &EvalContext{
		Inputs:     args,
		Resources:  make(map[string]any),
		Data:       make(map[string]any),
		Actions:    make(map[string]any),
		Libraries:  compositeBodyLibraries(boundary, e.Libraries),
		Assets:     assets,
		AssetCache: e.AssetCache,
		locals:     compositeLocalScope(boundary),
	}
	if rs.initializeComposite != nil {
		if err := rs.initializeComposite(callSite, scope); err != nil {
			return nil, err
		}
	}
	rs.composites[callSite] = scope
	return scope, nil
}

// templateAddress strips every `['key']` segment from addr to return
// the DAG-side address used to look the node up. Per-instance
// addresses inside a `@for-each` composite (`<x>['k']/<y>`) and
// leaf instance addresses (`<y>['k']`) both reduce to their
// template form.
func templateAddress(addr string) string {
	if template, ok := entryTemplate(addr); ok {
		return template
	}
	return addr
}

// DirectParent returns addr's parent state-ref segment path, or the
// empty string for a root segment. Unlike templateAddress, DirectParent
// preserves `['key']` segments so the result names a per-instance
// composite call site when one is present.
func DirectParent(addr string) string {
	if parent, ok := entryParent(addr); ok {
		return parent
	}
	if i := strings.LastIndex(addr, "/"); i >= 0 {
		return addr[:i]
	}
	return ""
}

func cloneSnapshot(s *state.Snapshot) *state.Snapshot {
	out := state.NewSnapshot(s.Factory, s.Stack)
	out.Outputs = cloneMap(s.Outputs)
	out.Entries = make([]*state.Entry, 0, len(s.Entries))
	for _, ent := range s.Entries {
		out.Entries = append(out.Entries, cloneEntry(ent))
	}
	return out
}

func cloneEntry(ent *state.Entry) *state.Entry {
	if ent == nil {
		return nil
	}
	out := *ent
	out.SensitiveInputs = append([]string(nil), ent.SensitiveInputs...)
	out.SensitiveOutputs = append([]string(nil), ent.SensitiveOutputs...)
	out.Inputs = cloneMap(ent.Inputs)
	out.Outputs = cloneMap(ent.Outputs)
	out.DependsOn = append([]string(nil), ent.DependsOn...)
	return &out
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMap(x)
	case []any:
		out := make([]any, len(x))
		for i, el := range x {
			out[i] = cloneValue(el)
		}
		return out
	default:
		return v
	}
}

// scopeMapForKind returns the scope map a node's value belongs in,
// chosen by its kind so references read it back under the matching
// address root. An unset kind (the zero value, as in tests that build a
// boundary directly) falls back to resources.
func scopeMapForKind(scope *EvalContext, kind NodeKind) map[string]any {
	switch kind {
	case NodeDataSource:
		return scope.Data
	case NodeAction:
		return scope.Actions
	default:
		return scope.Resources
	}
}

// forEachInstancesFor returns a `@for-each` node's evaluated iterable,
// memoized in rs by template address so every instance of the node
// shares one evaluation per run.
func forEachInstancesFor(
	rs *runState, templateAddr string, expr lang.Expr, scope *EvalContext,
) (map[string]any, error) {
	if instances, ok := rs.forEachInstances[templateAddr]; ok {
		return instances, nil
	}
	instances, err := evalForEach(expr, scope, rs.partialEvaluation)
	if err != nil {
		return nil, err
	}
	rs.forEachInstances[templateAddr] = instances
	return instances, nil
}

// evalForEach reduces a `@for-each:` expression to the iterable's
// key-value pairs. Only a map iterates: each instance needs a stable
// key, which a list's positions cannot provide.
func evalForEach(expr lang.Expr, scope *EvalContext, allowPending bool) (map[string]any, error) {
	var v any
	var err error
	if allowPending {
		var locals map[string]lang.Expr
		if scope != nil && scope.locals != nil {
			locals = scope.locals.exprs
		}
		v, _, err = partialValue(expr, scope, locals)
	} else {
		v, err = Eval(expr, scope)
	}
	if err != nil {
		return nil, diagnostic.Context("@for-each", err)
	}
	switch x := v.(type) {
	case map[string]any:
		return x, nil
	case []any:
		return nil, fmt.Errorf("@for-each: lists are not a valid iterable; use a map")
	}
	return nil, fmt.Errorf("@for-each: expected a map, got %s", lang.TypeMessage(v))
}

// childScopeWithEach returns a per-instance evaluation scope whose
// `@each.key` and `@each.value` bindings are set to the iteration's
// pair. The parent's Inputs, Resources, Data, Actions, and Libraries are
// shared by reference.
func childScopeWithEach(parent *EvalContext, key string, value any) *EvalContext {
	child := *parent
	child.Each = map[string]lang.EachValue{"@each": {Key: key, Value: value}}
	return &child
}

// instanceAddress appends a per-key suffix to a template address using
// the source-side `['<key>']` form so eval and state-lookup agree.
func instanceAddress(templateAddr, key string) string {
	if addr, ok := appendEntryKey(templateAddr, key); ok {
		return addr
	}
	return fmt.Sprintf("%s['%s']", templateAddr, key)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func addressValuePath(addr string) ([]string, bool) {
	parts, ok := addressParts(addr)
	if !ok || len(parts) < 2 {
		return nil, false
	}
	return parts[1:], true
}

func addressParts(addr string) ([]string, bool) {
	if ref, err := stateref.ParseStateRef(addr); err == nil {
		segment := ref.Segments[len(ref.Segments)-1]
		return []string{string(segment.Category), segment.Name}, true
	}
	seg := addr
	if parent := DirectParent(addr); parent != "" {
		seg = strings.TrimPrefix(addr, parent+"/")
	}
	seg, _ = splitInstanceAddress(seg)
	parts := strings.Split(seg, ".")
	if len(parts) < 2 {
		return nil, false
	}
	return parts, true
}

func bindingForNode(n *Node) *state.Binding {
	if n == nil || n.Alias == "" || n.Type == "" {
		return nil
	}
	return &state.Binding{Alias: n.Alias, LibraryPath: n.LibraryPath, Export: n.Type}
}

// evalBody evaluates an object literal body to a map[string]any of input
// values. `@`-prefixed meta keys are runtime metadata and skipped.
func evalBody(body lang.Expr, ec *EvalContext) (map[string]any, error) {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return nil, fmt.Errorf("body must be an object literal")
	}
	out := make(map[string]any, len(obj.Fields))
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		val, err := Eval(fld.Value, ec)
		if err != nil {
			return nil, diagnostic.Context(fmt.Sprintf("field %q", fld.Key.Name), err)
		}
		out[fld.Key.Name] = val
	}
	return out, nil
}

// evalConfigurationBody evaluates an internal configuration's body.
// An object literal evaluates field by field; any other expression
// evaluates whole and must produce an object.
func evalConfigurationBody(body lang.Expr, ec *EvalContext) (map[string]any, error) {
	if _, ok := body.(*lang.ObjectLit); ok {
		return evalBody(body, ec)
	}
	val, err := Eval(body, ec)
	if err != nil {
		return nil, err
	}
	obj, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("configuration body must evaluate to an object, got %s",
			lang.TypeMessage(val))
	}
	return obj, nil
}

// mergeAttrs returns the attribute view the reference layer sees for a
// node: its inputs with its outputs laid over them, so a computed or
// normalized output wins over a declared input of the same name. An
// input that has no same-named output stays readable at its declared
// value, which is why a downstream reference to a plain input resolves
// without the resource having to echo it into its output struct. Either
// map may be nil.
func mergeAttrs(inputs, outputs map[string]any) map[string]any {
	merged := make(map[string]any, len(inputs)+len(outputs))
	maps.Copy(merged, inputs)
	maps.Copy(merged, outputs)
	return merged
}

func getOrCreate(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		if mm, ok := v.(map[string]any); ok {
			return mm
		}
	}
	nm := make(map[string]any)
	m[key] = nm
	return nm
}

// mapify reduces a typed result struct to a map[string]any using its
// `ub` field tags (see ubFieldKey). Each field's value is canonicalized to
// the closed set of types unobin's runtime carries (string, int64,
// float64, bool, nil, []any, map[string]any), so named numeric types
// like time.Duration come back as int64 rather than leaking their
// Go-specific stringer through the renderer. Maps pass through; nil
// yields nil; anything else (non-struct, non-map) yields nil.
func mapify(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	m, _ := canonicalize(rv).(map[string]any)
	return m
}

// ubFieldKey returns the map key for a struct field under the ub tag
// convention, plus whether the field is skipped (`ub:"-"`). The
// decoder matches keys by the same rule, so a value mapify writes out
// reads back into the same field.
func ubFieldKey(field reflect.StructField) (key string, skip bool) {
	tag := ub.ParseTag(field.Tag.Get("ub"))
	if tag.Skip {
		return "", true
	}
	return tag.FieldName(field.Name), false
}

// canonicalize collapses a reflect.Value to one of the runtime's
// canonical Go types so downstream code (eval, render, state I/O)
// sees the same value forms regardless of whether the value came
// fresh from a library struct or back out of the state encoder. A
// named numeric type such as time.Duration normalizes to its
// underlying int64 (nanoseconds, in Duration's case).
func canonicalize(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(v.Uint())
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.String:
		return v.String()
	case reflect.Slice, reflect.Array:
		out := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			out[i] = canonicalize(v.Index(i))
		}
		return out
	case reflect.Map:
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			k := iter.Key()
			if k.Kind() != reflect.String {
				continue
			}
			out[k.String()] = canonicalize(iter.Value())
		}
		return out
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return canonicalize(v.Elem())
	case reflect.Struct:
		// A timestamp canonicalizes to the same text encoding/json
		// writes, so a value compares equal whether it came fresh from
		// a library or back out of a plan or state file.
		if t, ok := v.Interface().(time.Time); ok {
			return t.Format(time.RFC3339Nano)
		}
		rt := v.Type()
		out := make(map[string]any, rt.NumField())
		for i := range rt.NumField() {
			field := rt.Field(i)
			if !field.IsExported() {
				continue
			}
			name, skip := ubFieldKey(field)
			if skip {
				continue
			}
			out[name] = canonicalize(v.Field(i))
		}
		return out
	}
	return v.Interface()
}
