package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cloudboss/unobin/pkg/encoding/ub"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
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

// Executor wires together the parsed DAG, the imported libraries, the
// caller's inputs, and a state backend. It exposes three lifecycle
// methods: Plan computes a PlanStep slice against prior state without
// running any CRUD, ApplyPlan executes a previously computed plan,
// and Refresh reads each prior-state resource and writes back observed
// outputs. Store and Stack must always be set.
type Executor struct {
	DAG       *DAG
	Libraries map[string]*Library
	Inputs    map[string]any

	// Source is the parsed stack file. Static analysis passes (e.g.
	// sensitivity propagation at plan time) consult its top-level
	// blocks for declarations the DAG alone does not carry. May be
	// nil in test setups; analyses that need it degrade to no-op.
	Source *lang.File

	// Configurations is keyed first by the library's import alias and
	// then by the configuration alias declared in the stack file. Entries
	// are the value returned by cfg.ConfigurationType.New populated
	// by cfg.Decode. A nil map disables config routing and every CRUD
	// call sees a nil cfg argument.
	Configurations map[string]map[string]any

	// RawConfigurations holds the same operator-supplied bodies as
	// Configurations before decoding, by operator-facing field name.
	// A configuration.<alias>.<name> reference inside an internal
	// configuration's body reads from here.
	RawConfigurations map[string]map[string]any

	Store   state.Backend
	Factory state.FactoryInfo

	// Parallelism caps the number of in-flight resource, data, and
	// action steps during ApplyPlan. Zero or negative falls back to
	// DefaultParallelism.
	Parallelism int

	// Destroy makes Plan compute a teardown: every resource in prior
	// state is planned for destroy and no outputs are evaluated. The
	// source is still parsed and its configurations still resolve, so
	// the deletes use the right credentials.
	Destroy bool

	// Drain, when non-nil, lets the caller ask the scheduler to stop
	// dispatching new steps without canceling the apply context. The
	// runner closes this channel on SIGINT so in-flight CRUD calls
	// finish and their state writes commit; SIGTERM cancels the
	// context directly. A nil channel disables the drain signal.
	Drain <-chan struct{}

	// Events, when non-nil, receives one ApplyEvent per step stage
	// during ApplyPlan: start when the scheduler hands the step to a
	// worker, done or fail when the worker returns. The caller owns
	// the channel and is responsible for sizing the buffer and
	// closing it after ApplyPlan returns. A nil channel disables
	// event emission.
	Events chan<- ApplyEvent

	// internalConfigurations holds the decoded value of each internal
	// configuration once its node evaluates, keyed by node address.
	// Apply evaluates configurations on worker goroutines while
	// consumers read concurrently, so access goes through internalMu.
	internalConfigurations map[string]any
	internalMu             sync.Mutex

	// priorInternalConfigurations holds internal configurations
	// evaluated against the prior snapshot instead of the live run.
	// The state-entry paths read it: refresh, destroy deletes, and
	// orphan reads all operate on objects the last apply recorded, so
	// they use the configuration those objects were written with.
	priorInternalConfigurations map[string]any
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

func (e *Executor) priorInternalConfiguration(addr string) (any, bool) {
	e.internalMu.Lock()
	defer e.internalMu.Unlock()
	v, ok := e.priorInternalConfigurations[addr]
	return v, ok
}

// stateScope builds an evaluation context whose resource, data, and
// action values come from a prior snapshot, for evaluating internal
// configurations against what the last apply recorded. Only root
// entries seed it: configurations are defined at the factory root and
// cannot reference composite internals.
func (e *Executor) stateScope(prior *state.Snapshot, vars map[string]any) *EvalContext {
	scope := &EvalContext{
		Vars:           vars,
		Resources:      make(map[string]any),
		Data:           make(map[string]any),
		Actions:        make(map[string]any),
		Libraries:      e.Libraries,
		Configurations: e.RawConfigurations,
		locals:         newLocalScope(localsBlock(e.Source)),
	}
	if prior == nil {
		return scope
	}
	for _, ent := range prior.Entries {
		if strings.Contains(ent.Address, "/") {
			continue
		}
		tmpl, instKey := splitInstanceAddress(ent.Address)
		kind, _, _, _, ok := parseAddress(tmpl)
		if !ok {
			parts, found := addressParts(tmpl)
			if !found {
				continue
			}
			kind = NodeKind(parts[0])
		}
		target := scopeMapForKind(scope, NodeKind(kind))
		if target == nil {
			continue
		}
		attrs := mergeAttrs(ent.Inputs, ent.Outputs)
		if instKey == "" {
			seedAddress(target, tmpl, attrs)
		} else {
			seedAddressInstance(target, tmpl, instKey, attrs)
		}
	}
	return scope
}

// seedPriorInternalConfigurations evaluates every internal
// configuration against the prior snapshot and keeps the decoded
// values for the state-entry paths. A configuration whose sources are
// not all in state is skipped: a consumer that needs it sees a nil
// configuration, the same as an operator-side name that was never
// supplied. Live consumers never read these values; the plan walk and
// apply evaluate their own.
func (e *Executor) seedPriorInternalConfigurations(
	prior *state.Snapshot, vars map[string]any,
) error {
	if prior == nil || e.DAG == nil {
		return nil
	}
	var scope *EvalContext
	for _, n := range e.DAG.Nodes {
		if n.Kind != NodeConfiguration || e.configurationOverridden(n.Alias, n.Name) {
			continue
		}
		if scope == nil {
			scope = e.stateScope(prior, vars)
		}
		raw, err := evalConfigurationBody(n.Body, scope)
		if err != nil {
			if errors.Is(err, ErrEvalNotFound) {
				continue
			}
			return fmt.Errorf("%s: %w", n.Address, err)
		}
		lib, ok := e.librariesFor(n)[n.Alias]
		if !ok || lib.Configuration == nil {
			return fmt.Errorf("%s: library %q declares no configuration",
				n.Address, n.Alias)
		}
		decoded, err := cfg.Decode(lib.Configuration, raw)
		if err != nil {
			return fmt.Errorf("%s: %w", n.Address, err)
		}
		e.internalMu.Lock()
		if e.priorInternalConfigurations == nil {
			e.priorInternalConfigurations = map[string]any{}
		}
		e.priorInternalConfigurations[n.Address] = decoded
		e.internalMu.Unlock()
	}
	return nil
}

// effectiveParallelism returns the in-flight cap apply should honor.
func (e *Executor) effectiveParallelism() int {
	if e.Parallelism > 0 {
		return e.Parallelism
	}
	return DefaultParallelism
}

// resolvedConfigRef returns the (alias, configuration) pair the runtime
// resolves for a node; see the package function of the same name.
func (e *Executor) resolvedConfigRef(n *Node) (alias, configuration string) {
	return resolvedConfigRef(n, e.DAG.Nodes)
}

// resolvedConfigRef returns the (alias, configuration) pair a node's
// selection resolves to. The walk goes from the node up the composite
// chain, taking the first `@configurations:` entry that covers the
// node's import. If none does, the node's own `@configuration:`
// selection (or "default") applies.
func resolvedConfigRef(n *Node, nodes map[string]*Node) (alias, configuration string) {
	alias = n.Alias
	configuration = n.Configuration
	if configuration == "" {
		configuration = "default"
	}
	for parent := n.Composite; parent != ""; {
		c, ok := nodes[parent]
		if !ok {
			break
		}
		if mapped, has := c.ConfigurationsRemap[n.Alias]; has {
			alias = mapped.Alias
			configuration = mapped.Configuration
			break
		}
		parent = c.Composite
	}
	return alias, configuration
}

// pendingInternalConfig reports whether n's resolved selection names
// an internal configuration that has not evaluated this run, with the
// selection in alias.name form for step records. Reads gate on it at
// plan: a consumer must not reach its API with a nil configuration
// just because the configuration's own upstream is mid-change.
func (e *Executor) pendingInternalConfig(n *Node) (string, bool) {
	alias, configuration := e.resolvedConfigRef(n)
	if e.configurationOverridden(alias, configuration) {
		return "", false
	}
	addr := configurationAddress(alias, configuration)
	if _, internal := e.DAG.Nodes[addr]; !internal {
		return "", false
	}
	if _, ok := e.internalConfiguration(addr); ok {
		return "", false
	}
	return alias + "." + configuration, true
}

// configFor returns the decoded configuration to pass to a CRUD call
// on the given node. A stack-file override wins first. Otherwise, a
// selection the factory defines reads from the evaluated configuration
// table, and the rest reads from the operator's decoded configuration
// table.
func (e *Executor) configFor(n *Node) any {
	alias, configuration := e.resolvedConfigRef(n)
	if e.configurationOverridden(alias, configuration) {
		return e.lookupConfiguration(alias, configuration)
	}
	if e.DAG != nil {
		addr := configurationAddress(alias, configuration)
		if _, internal := e.DAG.Nodes[addr]; internal {
			v, _ := e.internalConfiguration(addr)
			return v
		}
	}
	return e.lookupConfiguration(alias, configuration)
}

// configRefString returns the "<alias>.<configuration>" a destroy or
// refresh should use to find credentials for n, or "" when n uses its
// own import's default configuration. The empty case is the common one
// and the resource address alone determines it, so it is left off the
// state entry to keep snapshots small.
func (e *Executor) configRefString(n *Node) string {
	alias, configuration := e.resolvedConfigRef(n)
	if alias == n.Alias && configuration == "default" {
		return ""
	}
	return alias + "." + configuration
}

// configForRef returns the configuration named by a state entry's
// recorded ref of the form "<alias>.<configuration>". An empty ref
// means the entry used its import's default configuration, so the
// entry's own import alias with the default applies. A ref naming an
// internal configuration reads the value evaluated from prior state,
// falling back to the live table when prior state had nothing for it.
//
// A recorded ref that resolves to nothing is an error rather than a
// nil configuration: the entry was written by a factory that had the
// configuration, so reaching the library without one means the
// running factory does not match the state, usually because the
// binary is older than the snapshot. An empty ref may still resolve
// to nil, the normal case for a library that declares no
// configuration.
func (e *Executor) configForRef(ref, fallbackAlias string) (any, error) {
	alias, configuration := fallbackAlias, "default"
	if ref != "" {
		if a, c, ok := strings.Cut(ref, "."); ok {
			alias, configuration = a, c
		}
	}
	if e.configurationOverridden(alias, configuration) {
		return e.lookupConfiguration(alias, configuration), nil
	}
	if e.DAG != nil {
		addr := configurationAddress(alias, configuration)
		if _, internal := e.DAG.Nodes[addr]; internal {
			if v, ok := e.priorInternalConfiguration(addr); ok {
				return v, nil
			}
			if v, ok := e.internalConfiguration(addr); ok {
				return v, nil
			}
			return nil, fmt.Errorf(
				"internal configuration %s.%s could not be evaluated from prior state",
				alias, configuration)
		}
	}
	v := e.lookupConfiguration(alias, configuration)
	if v == nil && ref != "" {
		return nil, fmt.Errorf(
			"state records configuration %s, which this factory neither defines nor "+
				"receives; the entry was written by a factory version that had it", ref)
	}
	return v, nil
}

func (e *Executor) configurationOverridden(alias, configuration string) bool {
	if e.Configurations == nil {
		return false
	}
	configurations, ok := e.Configurations[alias]
	if !ok {
		return false
	}
	_, ok = configurations[configuration]
	return ok
}

func (e *Executor) lookupConfiguration(alias, configuration string) any {
	if e.Configurations == nil {
		return nil
	}
	configurations, ok := e.Configurations[alias]
	if !ok {
		return nil
	}
	return configurations[configuration]
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

	// order is the DAG's topological order, computed once per run.
	// Plan's walk and per-instance composite expansion both follow it.
	order []string

	// composites holds one EvalContext per composite call site. Lazily
	// built when a node inside a composite first needs evaluation. Vars
	// in each scope are the call site args; Resources, Data, Actions
	// hold sibling outputs as the internals complete.
	composites map[string]*EvalContext

	// forEachInstances memoizes each `@for-each` node's evaluated
	// iterable by template address, so sibling instances share one
	// evaluation per run. Sound because the iterable's references are
	// dependencies of every instance: their values settle before the
	// first instance needs them and cannot change within the run.
	forEachInstances map[string]map[string]any

	// pendingReads queues per-resource Read calls collected during
	// Plan's serial walk so Plan can fan them out across workers
	// before finalizing decisions. Apply and Refresh leave this nil.
	pendingReads []*pendingRead

	// plannedByTemplate indexes the steps Plan's walk has emitted so
	// far by template address, so a later node can ask whether an
	// upstream it names has changes pending. The walk is topological,
	// so a node's upstreams are always indexed before it plans. Apply
	// and Refresh leave this nil.
	plannedByTemplate map[string][]*PlanStep

	// dependsOn maps each persisted step address to the addresses of
	// the other entries it depends on, in instance form. ApplyPlan
	// computes it once before dispatch and each apply method copies the
	// relevant slice onto the state entry it writes. Destroy ordering
	// reverses these edges.
	dependsOn map[string][]string

	// mu serializes mutation of eval, composites, next, and outputs,
	// plus calls to Store.Write / Store.SetCurrent. Apply takes the
	// lock around scope evaluation and around state writes; it is
	// released for the duration of each library's CRUD call so cloud
	// I/O runs in parallel across workers. Plan, Refresh, and the
	// state subcommands are single-threaded and do not contend.
	mu sync.Mutex
}

func (e *Executor) initRun() (*runState, error) {
	order, err := e.DAG.TopologicalOrder()
	if err != nil {
		return nil, err
	}
	rs := &runState{
		eval: &EvalContext{
			Vars:           e.Inputs,
			Resources:      make(map[string]any),
			Data:           make(map[string]any),
			Actions:        make(map[string]any),
			Libraries:      e.Libraries,
			Configurations: e.RawConfigurations,
			locals:         newLocalScope(localsBlock(e.Source)),
		},
		order:            order,
		outputs:          make(map[string]any),
		composites:       make(map[string]*EvalContext),
		forEachInstances: make(map[string]map[string]any),
		next:             state.NewSnapshot(e.Factory, e.Store.Stack()),
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

// librariesForAddress is the orphan-path equivalent of librariesFor: it
// resolves the import table for a state-only address whose source node
// has been removed. The direct parent call site (everything up to the
// last `/`) is consulted in the DAG; if its boundary is still present,
// its Libraries are used. Otherwise the executor's root Libraries is
// returned, which works whenever the parent composite type is still
// imported at the stack root.
func (e *Executor) librariesForAddress(addr string) map[string]*Library {
	if i := strings.LastIndex(addr, "/"); i >= 0 {
		callSite := addr[:i]
		if boundary, ok := e.DAG.Nodes[callSite]; ok && boundary.Libraries != nil {
			return boundary.Libraries
		}
	}
	return e.Libraries
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
		return nil, fmt.Errorf("composite %s: build parent scope: %w", callSite, err)
	}
	setAddr, instKey := splitInstanceAddress(callSite)
	bodyScope := parent
	if instKey != "" {
		instances, err := forEachInstancesFor(rs, setAddr, boundary.ForEach, parent)
		if err != nil {
			return nil, fmt.Errorf("composite %s: eval @for-each: %w", callSite, err)
		}
		value, ok := instances[instKey]
		if !ok {
			return nil, fmt.Errorf("composite %s: %w", callSite, ErrInstanceGone)
		}
		bodyScope = childScopeWithEach(parent, instKey, value)
	}
	args, err := evalBody(boundary.Body, bodyScope)
	if err != nil {
		return nil, fmt.Errorf("composite %s: eval call args: %w", callSite, err)
	}
	scope := &EvalContext{
		Vars:      args,
		Resources: make(map[string]any),
		Data:      make(map[string]any),
		Actions:   make(map[string]any),
		Libraries: compositeBodyLibraries(boundary, e.Libraries),
		locals:    newLocalScope(localsBlock(boundary.CompositeBody)),
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
	var out strings.Builder
	rest := addr
	for {
		start := strings.Index(rest, "['")
		if start < 0 {
			out.WriteString(rest)
			return out.String()
		}
		out.WriteString(rest[:start])
		rest = rest[start:]
		end := strings.Index(rest, "']")
		if end < 0 {
			out.WriteString(rest)
			return out.String()
		}
		rest = rest[end+2:]
	}
}

// DirectParent returns the substring before the last `/` in addr, or
// the empty string when addr has no `/`. Unlike templateAddress,
// DirectParent preserves `['key']` segments so the result names a
// per-instance composite call site when one is present.
func DirectParent(addr string) string {
	if i := strings.LastIndex(addr, "/"); i >= 0 {
		return addr[:i]
	}
	return ""
}

func (e *Executor) persist(rs *runState) (string, error) {
	rs.next.GeneratedAt = time.Now().UTC()
	rev, err := e.Store.Write(rs.next)
	if err != nil {
		return "", err
	}
	if err := e.Store.SetCurrent(rev); err != nil {
		return "", err
	}
	return rev, nil
}

func (e *Executor) prepareApplySnapshot(rs *runState) {
	if rs.prior == nil {
		return
	}
	rs.next = cloneSnapshot(rs.prior)
	rs.next.Factory = e.Factory
	rs.next.Stack = e.Store.Stack()
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

func upsertEntry(snap *state.Snapshot, ent *state.Entry) {
	for i, existing := range snap.Entries {
		if existing.Address == ent.Address {
			snap.Entries[i] = ent
			return
		}
	}
	snap.Entries = append(snap.Entries, ent)
}

func removeEntry(snap *state.Snapshot, address string) {
	for i, ent := range snap.Entries {
		if ent.Address != address {
			continue
		}
		snap.Entries = append(snap.Entries[:i], snap.Entries[i+1:]...)
		return
	}
}

func pruneStateEntries(snap *state.Snapshot, steps []PlanStep) {
	keep := make(map[string]bool, len(steps))
	for _, step := range steps {
		if step.Decision == DecisionDestroy {
			continue
		}
		if step.Composite {
			keep[step.Address] = true
			continue
		}
		switch step.Kind {
		case NodeAction, NodeResource, NodeData:
			keep[step.Address] = true
		}
	}
	out := snap.Entries[:0]
	for _, ent := range snap.Entries {
		if keep[ent.Address] {
			out = append(out, ent)
		}
	}
	snap.Entries = out
}

// scopeMapForKind returns the scope map a node's value belongs in,
// chosen by its kind so references read it back under the matching
// address root. An unset kind (the zero value, as in tests that build a
// boundary directly) falls back to resources.
func scopeMapForKind(scope *EvalContext, kind NodeKind) map[string]any {
	switch kind {
	case NodeData:
		return scope.Data
	case NodeAction:
		return scope.Actions
	default:
		return scope.Resources
	}
}

// finalizeComposite closes a composite call site after its
// internals have finished. It reads the composite body's `outputs:`
// block against the per-instance scope (a non-for-each composite
// has one instance, addressed at the template address itself),
// exposes those outputs at the call site address in the boundary's
// enclosing scope so its parent can reach them, and writes one
// EntryLibraryCall record. instAddr is the address actually being
// finalized: equal to n.Address for a plain composite, with a
// trailing `['key']` for a `@for-each` instance. inputs is the call
// site arg map evaluated for this instance.
func (e *Executor) finalizeComposite(
	rs *runState, n *Node, instAddr string, inputs map[string]any,
	sensitiveInputs, sensitiveOutputs []string,
) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	scope, err := e.ensureCompositeScope(rs, instAddr)
	if err != nil {
		return err
	}
	outputs, err := evalCompositeOutputs(n.CompositeBody, scope)
	if err != nil {
		return err
	}
	parent, err := e.enclosingScope(rs, instAddr)
	if err != nil {
		return err
	}
	_, instKey := splitInstanceAddress(instAddr)
	target := scopeMapForKind(parent, n.Kind)
	if instKey == "" {
		storeNested(target, n, outputs)
	} else {
		seedAddressInstance(target, n.Address, instKey, outputs)
	}
	upsertEntry(rs.next, &state.Entry{
		Address:          instAddr,
		Type:             state.EntryLibraryCall,
		Library:          n.Alias,
		LibraryType:      n.Type,
		Selector:         selectorForNode(n),
		Inputs:           inputs,
		Outputs:          outputs,
		SensitiveInputs:  sensitiveInputs,
		SensitiveOutputs: sensitiveOutputs,
		DependsOn:        rs.dependsOn[instAddr],
	})
	return nil
}

// evalCompositeOutputs reads the composite body's `outputs:` block
// and reduces each field against the given scope. Returns nil when
// the body has no outputs block.
func evalCompositeOutputs(body *lang.File, scope *EvalContext) (map[string]any, error) {
	outBlock := lang.TopLevelBlock(body, "outputs")
	if outBlock == nil {
		return nil, nil
	}
	out := make(map[string]any, len(outBlock.Fields))
	for _, fld := range outBlock.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		inner := lang.OutputValueExpr(fld.Value)
		if inner == nil {
			return nil, fmt.Errorf("composite output %q: missing wrapper", fld.Key.Name)
		}
		val, err := Eval(inner, scope)
		if err != nil {
			return nil, fmt.Errorf("composite output %q: %w", fld.Key.Name, err)
		}
		out[fld.Key.Name] = val
	}
	return out, nil
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
	instances, err := evalForEach(expr, scope)
	if err != nil {
		return nil, err
	}
	rs.forEachInstances[templateAddr] = instances
	return instances, nil
}

// evalForEach reduces a `@for-each:` expression to the iterable's
// key-value pairs. Only a map iterates: each instance needs a stable
// key, which a list's positions cannot provide.
func evalForEach(expr lang.Expr, scope *EvalContext) (map[string]any, error) {
	v, err := Eval(expr, scope)
	if err != nil {
		return nil, fmt.Errorf("@for-each: %w", err)
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
// pair. The parent's Vars, Resources, Data, Actions, and Libraries are
// shared by reference.
func childScopeWithEach(parent *EvalContext, key string, value any) *EvalContext {
	child := *parent
	child.Each = map[string]lang.EachValue{"@each": {Key: key, Value: value}}
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
	slices.Sort(keys)
	return keys
}

// sameInputs compares two input maps by their canonical JSON form so a
// state round trip, which renders integers as floats, doesn't show up as
// a change.
func sameInputs(a, b map[string]any) bool {
	// A nil map and an empty map are the same input set, but they
	// marshal differently (null vs {}), so the byte compare below
	// would call an empty body changed after a plan-file round trip.
	if len(a) == 0 && len(b) == 0 {
		return true
	}
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

// changedReplaceFields returns the replace-forcing fields whose canonical
// JSON value differs between prior and current inputs. A non-empty result
// means the resource must be replaced and names the fields that forced it.
func changedReplaceFields(replaceFields []string, prior, current map[string]any) []string {
	var changed []string
	for _, field := range replaceFields {
		if !sameValue(prior[field], current[field]) {
			changed = append(changed, field)
		}
	}
	return changed
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

// parseAddress reads the inner-most node segment of addr and splits it
// into its kind root, alias, type, and name. addr may be a root
// address (`resource.aws.vpc.this`) or a composite-internal address whose
// segments are `/`-joined; only the final segment is parsed, so the node
// is read relative to its direct enclosing scope. A trailing `@for-each`
// instance key on that segment is ignored.
func parseAddress(addr string) (kind NodeKind, alias, typeName, name string, ok bool) {
	parts, ok := addressParts(addr)
	if !ok || len(parts) != 4 {
		return "", "", "", "", false
	}
	return NodeKind(parts[0]), parts[1], parts[2], parts[3], true
}

func addressValuePath(addr string) ([]string, bool) {
	parts, ok := addressParts(addr)
	if !ok || len(parts) < 2 {
		return nil, false
	}
	return parts[1:], true
}

func addressParts(addr string) ([]string, bool) {
	seg := addr
	if i := strings.LastIndex(seg, "/"); i >= 0 {
		seg = seg[i+1:]
	}
	seg, _ = splitInstanceAddress(seg)
	parts := strings.Split(seg, ".")
	if len(parts) < 2 {
		return nil, false
	}
	return parts, true
}

func selectorForNode(n *Node) *state.Selector {
	if n == nil || n.Alias == "" || n.Type == "" {
		return nil
	}
	return &state.Selector{Alias: n.Alias, Export: n.Type}
}

func selectorFromEntry(ent *state.Entry) *state.Selector {
	if ent == nil {
		return nil
	}
	if ent.Selector != nil {
		return cloneSelector(ent.Selector)
	}
	if ent.Library != "" && ent.LibraryType != "" {
		return &state.Selector{Alias: ent.Library, Export: ent.LibraryType}
	}
	_, alias, typeName, _, ok := parseAddress(ent.Address)
	if !ok {
		return nil
	}
	return &state.Selector{Alias: alias, Export: typeName}
}

func cloneSelector(sel *state.Selector) *state.Selector {
	if sel == nil {
		return nil
	}
	return &state.Selector{Alias: sel.Alias, Export: sel.Export}
}

func selectorParts(sel *state.Selector) (alias, typeName string, ok bool) {
	if sel == nil || sel.Alias == "" || sel.Export == "" {
		return "", "", false
	}
	return sel.Alias, sel.Export, true
}

func sameSelector(a, b *state.Selector) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Alias == b.Alias && a.Export == b.Export
}

func entrySelectorParts(ent *state.Entry) (alias, typeName string, ok bool) {
	if alias, typeName, ok := selectorParts(selectorFromEntry(ent)); ok {
		return alias, typeName, true
	}
	return "", "", false
}

func stepSelectorParts(step *PlanStep) (alias, typeName string, ok bool) {
	if step == nil {
		return "", "", false
	}
	if alias, typeName, ok := selectorParts(step.Selector); ok {
		return alias, typeName, true
	}
	_, alias, typeName, _, ok = parseAddress(step.Address)
	return alias, typeName, ok
}

func (e *Executor) resourceRegistrationForSelector(
	addr string,
	sel *state.Selector,
) (ResourceRegistration, string, error) {
	alias, typeName, ok := selectorParts(sel)
	if !ok {
		return nil, "", fmt.Errorf("missing selector for %q", addr)
	}
	lib, ok := e.librariesForAddress(addr)[alias]
	if !ok {
		return nil, "", fmt.Errorf("library %q is not imported", alias)
	}
	rt, ok := lib.Resources[typeName]
	if !ok {
		return nil, "", fmt.Errorf("library %s has no resource %q", alias, typeName)
	}
	return rt, alias, nil
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

// storeNested writes value at the node's reference path.
func storeNested(target map[string]any, n *Node, value map[string]any) {
	seedAddress(target, n.Address, value)
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
