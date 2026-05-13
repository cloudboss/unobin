package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/state"
)

// Executor wires together the parsed DAG, the imported modules, the
// caller's inputs, and a state backend. It exposes three lifecycle
// methods: Plan computes a PlanStep slice against prior state without
// running any CRUD, ApplyPlan executes a previously computed plan,
// and Refresh reads each prior-state resource and writes back observed
// outputs. Store and Stack must always be set.
type Executor struct {
	DAG     *DAG
	Modules map[string]*Module
	Inputs  map[string]any

	Store state.Backend
	Stack state.StackInfo
}

// ExecResult is what the Executor produces: the outputs map, the
// Action and Data tables populated during the run, and the rev of the
// snapshot written (empty when no Store was configured).
type ExecResult struct {
	Outputs    map[string]any
	Actions    map[string]any
	Data       map[string]any
	WrittenRev string
}

type runState struct {
	eval    *EvalContext
	outputs map[string]any
	prior   *state.Snapshot
	next    *state.Snapshot

	// composites holds one EvalContext per composite call site. Lazily
	// built when a node inside a composite first needs evaluation. Vars
	// in each scope are the call site args; Resources, Data, Actions
	// hold sibling outputs as the internals complete.
	composites map[string]*EvalContext
}

func (e *Executor) initRun() (*runState, error) {
	rs := &runState{
		eval: &EvalContext{
			Vars:      e.Inputs,
			Resources: make(map[string]any),
			Data:      make(map[string]any),
			Actions:   make(map[string]any),
			Modules:   e.Modules,
		},
		outputs:    make(map[string]any),
		composites: make(map[string]*EvalContext),
		next:       state.NewSnapshot(e.Stack, e.Store.DeploymentID()),
	}
	prior, err := e.Store.Current()
	if err != nil && !errors.Is(err, state.ErrNoCurrent) {
		return nil, err
	}
	rs.prior = prior
	return rs, nil
}

// scopeFor returns the EvalContext n's body should be evaluated
// against. Root scope for nodes outside a composite, the composite's
// own scope otherwise. The composite scope's Vars carry the call site
// args and its Resources/Data/Actions hold sibling outputs.
func (e *Executor) scopeFor(rs *runState, n *Node) (*EvalContext, error) {
	if n.Composite == "" {
		return rs.eval, nil
	}
	return e.ensureCompositeScope(rs, n.Composite)
}

// modulesFor returns the import table the runtime should resolve n's
// module alias against. Top-level nodes use the executor's root
// Modules; composite-internal nodes use their boundary's Modules so a
// composite stays self-contained. Falls back to e.Modules when a
// composite has no Modules populated, preserving backward
// compatibility for direct test construction.
func (e *Executor) modulesFor(n *Node) map[string]*Module {
	if n.Composite == "" {
		return e.Modules
	}
	if boundary, ok := e.DAG.Nodes[n.Composite]; ok && boundary.Modules != nil {
		return boundary.Modules
	}
	return e.Modules
}

// compositeBodyModules returns the import table the composite's own
// body (internals and outputs) should resolve aliases against. The
// boundary node carries the composite's Modules; an unset table falls
// back to the executor's root for test compositions that don't set it.
func compositeBodyModules(boundary *Node, fallback map[string]*Module) map[string]*Module {
	if boundary.Modules != nil {
		return boundary.Modules
	}
	return fallback
}

// modulesForAddress is the orphan-path equivalent of modulesFor: it
// resolves the import table for a state-only address whose source node
// has been removed. The direct parent call site (everything up to the
// last `/`) is consulted in the DAG; if its boundary is still present,
// its Modules are used. Otherwise the executor's root Modules is
// returned, which works whenever the parent composite type is still
// imported at the stack root.
func (e *Executor) modulesForAddress(addr string) map[string]*Module {
	if i := strings.LastIndex(addr, "/"); i >= 0 {
		callSite := addr[:i]
		if boundary, ok := e.DAG.Nodes[callSite]; ok && boundary.Modules != nil {
			return boundary.Modules
		}
	}
	return e.Modules
}

func (e *Executor) ensureCompositeScope(rs *runState, callSite string) (*EvalContext, error) {
	if scope, ok := rs.composites[callSite]; ok {
		return scope, nil
	}
	boundary, ok := e.DAG.Nodes[callSite]
	if !ok {
		return nil, fmt.Errorf("composite %s: boundary node not in DAG", callSite)
	}
	parent, err := e.scopeFor(rs, boundary)
	if err != nil {
		return nil, fmt.Errorf("composite %s: build parent scope: %w", callSite, err)
	}
	args, err := evalBody(boundary.Body, parent)
	if err != nil {
		return nil, fmt.Errorf("composite %s: eval call args: %w", callSite, err)
	}
	scope := &EvalContext{
		Vars:      args,
		Resources: make(map[string]any),
		Data:      make(map[string]any),
		Actions:   make(map[string]any),
		Modules:   compositeBodyModules(boundary, e.Modules),
	}
	rs.composites[callSite] = scope
	return scope, nil
}

func (e *Executor) persist(rs *runState) (string, error) {
	rev, err := e.Store.Write(rs.next)
	if err != nil {
		return "", err
	}
	if err := e.Store.SetCurrent(rev); err != nil {
		return "", err
	}
	return rev, nil
}

// finalizeComposite closes a composite call site after its
// internals have finished. It reads the composite body's `outputs:`
// block against the composite scope, exposes those outputs at the
// call site address in the boundary's enclosing scope so its parent
// can reach them, and writes one EntryModuleCall record holding the
// given inputs and the computed outputs. Inputs is the call site arg
// map; pass scope.Vars when called from Run, step.Inputs when called
// from ApplyPlan.
func (e *Executor) finalizeComposite(rs *runState, n *Node, inputs map[string]any) error {
	scope, err := e.ensureCompositeScope(rs, n.Address)
	if err != nil {
		return err
	}
	outputs, err := evalCompositeOutputs(n.CompositeBody, scope)
	if err != nil {
		return err
	}
	parent, err := e.scopeFor(rs, n)
	if err != nil {
		return err
	}
	storeNested(parent.Resources, n, outputs)
	rs.next.Entries = append(rs.next.Entries, &state.Entry{
		Address:    n.Address,
		Type:       state.EntryModuleCall,
		Module:     n.NS,
		ModuleType: n.Type,
		Inputs:     inputs,
		Outputs:    outputs,
	})
	return nil
}

// evalCompositeOutputs reads the composite body's `outputs:` block
// and reduces each field against the given scope. Returns nil when
// the body has no outputs block.
func evalCompositeOutputs(body *lang.File, scope *EvalContext) (map[string]any, error) {
	if body == nil || body.Body == nil {
		return nil, nil
	}
	var outBlock *lang.ObjectLit
	for _, fld := range body.Body.Fields {
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == "outputs" {
			obj, ok := fld.Value.(*lang.ObjectLit)
			if !ok {
				return nil, fmt.Errorf("composite outputs: expected an object")
			}
			outBlock = obj
			break
		}
	}
	if outBlock == nil {
		return nil, nil
	}
	out := make(map[string]any, len(outBlock.Fields))
	for _, fld := range outBlock.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		val, err := Eval(fld.Value, scope)
		if err != nil {
			return nil, fmt.Errorf("composite output %q: %w", fld.Key.Name, err)
		}
		out[fld.Key.Name] = val
	}
	return out, nil
}

// evalForEach reduces a `@for-each:` expression to the iterable's
// key-value pairs. A map literal evaluates to map[string]any directly.
// A list evaluates to []any and is rejected per the language design
// (sets are the only sequence form; lists are not a valid iterable).
func evalForEach(expr lang.Expr, scope *EvalContext) (map[string]any, error) {
	v, err := Eval(expr, scope)
	if err != nil {
		return nil, fmt.Errorf("@for-each: %w", err)
	}
	switch x := v.(type) {
	case map[string]any:
		return x, nil
	case []any:
		return nil, fmt.Errorf("@for-each: lists are not a valid iterable; use a map or a set")
	}
	return nil, fmt.Errorf("@for-each: expected a map, got %T", v)
}

// childScopeWithEach returns a per-instance evaluation scope whose
// `@each.key` and `@each.value` bindings are set to the iteration's
// pair. The parent's Vars, Resources, Data, Actions, and Modules are
// shared by reference.
func childScopeWithEach(parent *EvalContext, key string, value any) *EvalContext {
	child := *parent
	child.EachKey = key
	child.EachValue = value
	child.ForEach = true
	return &child
}

// instanceAddress appends a per-key suffix to a template address using
// the source-side `['<key>']` form so eval and state-lookup agree.
func instanceAddress(templateAddr, key string) string {
	return fmt.Sprintf("%s['%s']", templateAddr, key)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sameInputs compares two input maps by their canonical JSON form so a
// state round trip, which renders integers as floats, doesn't show up as
// a change.
func sameInputs(a, b map[string]any) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aj, bj)
}

// needsReplace reports whether any field in r.ReplaceFields() has a
// different canonical JSON value between prior and current inputs.
func needsReplace(r Resource, prior, current map[string]any) bool {
	for _, field := range r.ReplaceFields() {
		if !sameValue(prior[field], current[field]) {
			return true
		}
	}
	return false
}

func sameValue(a, b any) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aj, bj)
}

func parseResourceAddress(addr string) (ns, typeName, name string, ok bool) {
	parts := strings.SplitN(addr, ".", 4)
	if len(parts) != 4 || parts[0] != "resource" {
		return "", "", "", false
	}
	return parts[1], parts[2], parts[3], true
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
			return nil, fmt.Errorf("field %q: %w", fld.Key.Name, err)
		}
		out[fld.Key.Name] = val
	}
	return out, nil
}

// storeNested writes value at target[ns][type][name], creating intermediate
// maps as needed.
func storeNested(target map[string]any, n *Node, value map[string]any) {
	ns := getOrCreate(target, n.NS)
	typeMap := getOrCreate(ns, n.Type)
	typeMap[n.Name] = value
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
// `mapstructure` field tags. Maps pass through; nil yields nil; anything
// else (non-struct, non-map) yields nil.
func mapify(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	rt := rv.Type()
	out := make(map[string]any, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}
		name := field.Tag.Get("mapstructure")
		if name == "" {
			name = field.Name
		}
		if name == "-" {
			continue
		}
		out[name] = rv.Field(i).Interface()
	}
	return out
}
