package runtime

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// planEvalBody evaluates a body field by field against the plan-time
// scope. A field that resolves cleanly contributes its evaluated value
// to inputs. A field whose evaluation hits ErrEvalNotFound (because an
// upstream resource, action, or data source has not run yet) gets nil
// in inputs and its referenced source addresses are recorded in
// unresolved so the renderer can show `<resource.X.field>` rather than
// a misleading null. Apply re-evaluates the body against the live
// scope and returns a real error if the reference is genuinely
// invalid.
func planEvalBody(body lang.Expr, ec *EvalContext) (map[string]any, map[string][]string, error) {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return nil, nil, fmt.Errorf("body must be an object literal")
	}
	inputs := make(map[string]any, len(obj.Fields))
	var unresolved map[string][]string
	var locals map[string]lang.Expr
	if ec != nil && ec.locals != nil {
		locals = ec.locals.exprs
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		val, err := Eval(fld.Value, ec)
		if err == nil {
			inputs[fld.Key.Name] = val
			continue
		}
		if !errors.Is(err, ErrEvalNotFound) {
			return nil, nil, fmt.Errorf("field %q: %w", fld.Key.Name, err)
		}
		inputs[fld.Key.Name] = nil
		refs := deferredRefs(fld.Value, locals)
		if len(refs) > 0 {
			if unresolved == nil {
				unresolved = map[string][]string{}
			}
			unresolved[fld.Key.Name] = refs
		}
	}
	return inputs, unresolved, nil
}

// Decision tags one node's planned action.
type Decision string

const (
	DecisionCreate  Decision = "create"
	DecisionUpdate  Decision = "update"
	DecisionReplace Decision = "replace"
	DecisionDestroy Decision = "destroy"
	DecisionNoOp    Decision = "no-op"
	DecisionRerun   Decision = "rerun"
	DecisionSkip    Decision = "skip"
	DecisionRead    Decision = "read"
	DecisionEval    Decision = "eval"
)

// PlanStep records one node's planned action. For resources, Inputs is
// the evaluated body. PriorOutputs is what state holds (nil for create
// or destroy of a resource that is not found). ObservedOutputs is what
// Resource.Read returned at plan time; it differs from PriorOutputs
// when the resource has drifted out of band. For actions, TriggerHash
// is the hash that determines whether to rerun or skip.
//
// UnresolvedInputs names the input fields whose plan-time evaluation
// hit a forward reference (an upstream node with no prior state). Each
// entry maps the field name to the source-side dot paths the body
// reads from. Apply re-evaluates these against the live scope.
type PlanStep struct {
	Address string   `json:"address"`
	Kind    NodeKind `json:"kind"`

	// Composite marks a step whose apply finalizes a composite call
	// site (a boundary) rather than a primitive leaf. A boundary's Kind
	// is its own resource/data/action kind, so the runtime cannot
	// tell it from a leaf by Kind alone; on a Node that distinction is
	// the expanded CompositeBody (Node.IsComposite), but a step has no
	// body, so the bit is stored explicitly in the plan file.
	Composite bool `json:"composite,omitempty"`

	Decision         Decision            `json:"decision"`
	Inputs           map[string]any      `json:"inputs,omitempty"`
	UnresolvedInputs map[string][]string `json:"unresolved-inputs,omitempty"`
	PriorOutputs     map[string]any      `json:"prior-outputs,omitempty"`
	ObservedOutputs  map[string]any      `json:"observed-outputs,omitempty"`
	TriggerHash      string              `json:"trigger-hash,omitempty"`

	// Configuration carries a destroy step's recorded library
	// configuration ref ("<alias>.<configuration>") from prior state, so
	// apply deletes against the same credentials the resource was created
	// with.
	Configuration string `json:"configuration,omitempty"`

	// DependsOn carries a destroy step's recorded dependencies from
	// prior state. Apply reverses these edges so a resource is deleted
	// before the resources it depended on.
	DependsOn []string `json:"depends-on,omitempty"`

	// AlreadyGone marks a destroy step whose resource was already
	// absent when the plan read it. Apply drops the entry from state
	// without calling Delete.
	AlreadyGone bool `json:"already-gone,omitempty"`

	// SensitiveInputs names the input fields whose value expression
	// reads from any sensitive source. Renderers replace the value
	// with a placeholder rather than printing the secret.
	SensitiveInputs []string `json:"sensitive-inputs,omitempty"`

	// SensitiveOutputs names the output fields of this step that are
	// sensitive. For a primitive resource/action it comes from the
	// library schema's tagged fields; for a composite call site it
	// comes from the composite type's `@sensitive` markers and from
	// propagation through its body.
	SensitiveOutputs []string `json:"sensitive-outputs,omitempty"`
}

// Drift reports whether the resource's observed outputs differ from
// the outputs in prior state. False for steps with no prior or no
// observation (Create, Destroy, Gone).
func (s *PlanStep) Drift() bool {
	if len(s.PriorOutputs) == 0 || len(s.ObservedOutputs) == 0 {
		return false
	}
	return !sameInputs(s.PriorOutputs, s.ObservedOutputs)
}

// Gone reports whether a resource with prior state was missing in the
// cloud at plan time. Encoded as Create with PriorOutputs set.
func (s *PlanStep) Gone() bool {
	return s.Kind == NodeResource &&
		s.Decision == DecisionCreate &&
		len(s.PriorOutputs) > 0
}

// Plan is the readonly result of computing what an apply would do.
// StateRev is the snapshot rev the plan was computed against. Apply
// rejects the plan when the current rev no longer matches. Inputs
// captures the validated root inputs so apply can rebuild the same
// eval scope without re-reading config.ub. RawConfigurations carries
// the raw per-library configuration maps (keyed by import alias then
// alias name) so apply re-decodes them through the same code path
// rather than re-reading the config file.
type Plan struct {
	Factory           state.FactoryInfo
	Stack             string
	StateRev          string
	Inputs            map[string]any
	RawConfigurations map[string]map[string]any
	Steps             []*PlanStep

	// Backend names the state backend the plan was computed against,
	// so apply can reconstruct the same backend without re-reading
	// config.ub. A nil value means the resolver's default (the local
	// backend under .unobin/state) was used.
	Backend *StateRef

	// Parallelism is the in-flight cap apply should honor. Zero means
	// the runtime's DefaultParallelism applies.
	Parallelism int

	// Destroy marks a teardown plan: every step is a destroy and apply
	// evaluates no outputs.
	Destroy bool
}

// Plan walks the DAG against prior state and returns the planned
// actions per node. Resources with prior state get their Read method
// invoked so the plan can report drift; no CRUD methods (Create,
// Update, Replace, Delete) are called and no actions run. Inputs that
// reference outputs of nodes about to run are evaluated against the
// prior state where available.
func (e *Executor) Plan(ctx context.Context) (*Plan, error) {
	if e.Store == nil {
		return nil, errors.New("executor: Store is required")
	}
	if err := e.checkConfigurations(); err != nil {
		return nil, err
	}
	order, err := e.DAG.TopologicalOrder()
	if err != nil {
		return nil, err
	}
	rs, err := e.initRun()
	if err != nil {
		return nil, err
	}
	stateRev, _ := e.Store.CurrentRev()

	plan := &Plan{
		Factory:     e.Factory,
		Stack:       e.Store.Stack(),
		StateRev:    stateRev,
		Inputs:      e.Inputs,
		Parallelism: e.Parallelism,
		Destroy:     e.Destroy,
	}

	// Seed the EvalContext with prior outputs so downstream evaluation
	// has something to bind to even when an upstream node would change.
	if err := e.seedFromPriorState(rs); err != nil {
		return nil, err
	}

	sensitivity := newSensitivityAnalyzer(e.Source, e.Libraries, e.DAG)

	// A destroy plan wants nothing from source, so the desired-state
	// walk is skipped. With no live addresses, every prior leaf below
	// becomes a destroy step.
	liveAddresses := make(map[string]bool)
	var constraintErrs []error
	if !e.Destroy {
		for _, addr := range order {
			node := e.DAG.Nodes[addr]
			steps, err := e.planNodeSteps(rs, node)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", addr, err)
			}
			for _, step := range steps {
				step.SensitiveInputs = sensitivity.sensitiveInputs(node.Body, node.Composite)
				step.SensitiveOutputs = sensitivity.sensitiveOutputs(node)
				plan.Steps = append(plan.Steps, step)
				liveAddresses[step.Address] = true
				if err := e.seedStepAttrs(rs, step); err != nil {
					return nil, fmt.Errorf("%s: %w", step.Address, err)
				}
				constraintErrs = append(constraintErrs, e.checkStepConstraints(step)...)
				constraintErrs = append(constraintErrs, e.checkCompositeConstraints(rs, step)...)
			}
		}

		if err := e.runPendingReads(ctx, rs); err != nil {
			return nil, err
		}
		if err := e.finalizePendingReads(rs); err != nil {
			return nil, err
		}
		upgradeActionRerun(plan.Steps, e.DAG, newScopeLocals(e.Source, e.DAG.Nodes))
	}

	if len(constraintErrs) > 0 {
		return nil, errors.Join(constraintErrs...)
	}

	// Orphans: prior entries with no live address in this plan become
	// destroy steps. A normal plan only destroys orphaned leaf
	// resources; action and library-call records are cleaned up by
	// pruning. A destroy plan removes every record, so it emits a step
	// for each entry type.
	if rs.prior != nil {
		for _, prior := range rs.prior.Entries {
			if liveAddresses[prior.Address] {
				continue
			}
			kind, composite, ok := destroyEntryKind(prior.Type)
			if !ok {
				continue
			}
			if !e.Destroy && prior.Type != state.EntryLeaf {
				continue
			}
			plan.Steps = append(plan.Steps, &PlanStep{
				Address:       prior.Address,
				Kind:          kind,
				Composite:     composite,
				Decision:      DecisionDestroy,
				Inputs:        prior.Inputs,
				PriorOutputs:  prior.Outputs,
				Configuration: prior.Configuration,
				DependsOn:     prior.DependsOn,
			})
		}
	}
	if err := e.readDestroySteps(ctx, plan.Steps); err != nil {
		return nil, err
	}
	return plan, nil
}

// destroyEntryKind maps a state entry type to the node kind its destroy
// step takes and whether that step is a composite boundary. Leaf
// entries delete a real resource; action and library-call records have
// no external lifecycle and are only removed from state. A library-call
// record does not remember its resource/data/action kind, but a
// destroy boundary never reads its kind (it is only removed), so the
// kind is left empty and the composite bit alone routes it.
func destroyEntryKind(t state.EntryType) (kind NodeKind, composite, ok bool) {
	switch t {
	case state.EntryLeaf:
		return NodeResource, false, true
	case state.EntryAction:
		return NodeAction, false, true
	case state.EntryLibraryCall:
		return "", true, true
	}
	return "", false, false
}

// readDestroySteps reads each destroy step's resource so the plan can
// tell an already-absent resource from one that still needs deleting.
// A read that comes back not-found marks the step AlreadyGone, so apply
// drops the entry without calling Delete. Reads run in parallel up to
// effectiveParallelism. This keeps destroy on the same "plan reads
// every resource with prior state" footing as a normal plan, and
// covers orphan destroys in a normal plan as well as a full teardown.
func (e *Executor) readDestroySteps(ctx context.Context, steps []*PlanStep) error {
	var jobs []*PlanStep
	for _, s := range steps {
		// Only resources have something in the world to read; action
		// and library-call records are removed without a read.
		if s.Decision == DecisionDestroy && s.Kind == NodeResource {
			jobs = append(jobs, s)
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	gone := make([]bool, len(jobs))
	errs := make([]error, len(jobs))
	sem := make(chan struct{}, e.effectiveParallelism())
	var wg sync.WaitGroup
	for i, s := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, s *PlanStep) {
			defer func() { <-sem; wg.Done() }()
			gone[i], errs[i] = e.readDestroyTarget(ctx, s)
		}(i, s)
	}
	wg.Wait()
	for i, s := range jobs {
		if errs[i] != nil {
			return fmt.Errorf("%s: read: %w", s.Address, errs[i])
		}
		s.AlreadyGone = gone[i]
	}
	return nil
}

// readDestroyTarget resolves a destroy step's library from its address
// and reads the resource, reporting whether it is already gone. It
// needs no DAG node, so it works for an orphan whose source has been
// removed as well as for a full teardown.
func (e *Executor) readDestroyTarget(ctx context.Context, step *PlanStep) (bool, error) {
	_, alias, typeName, _, ok := parseAddress(step.Address)
	if !ok {
		return false, fmt.Errorf("malformed address %q", step.Address)
	}
	lib, ok := e.librariesForAddress(step.Address)[alias]
	if !ok {
		return false, fmt.Errorf("library %q is not imported", alias)
	}
	rt, ok := lib.Resources[typeName]
	if !ok {
		return false, fmt.Errorf("library %s has no resource %q", alias, typeName)
	}
	_, err := readObserved(ctx, rt,
		e.configForRef(step.Configuration, alias), step.Inputs, step.PriorOutputs)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// planNodeSteps wraps planNode so a single per-node planner can emit
// multiple steps. A `@for-each` template (resource, action, data
// source, or composite) fans out into one step per instance;
// everything else returns a one-element slice (or nil to skip the
// node). Nodes inside a `@for-each` composite are skipped here: their
// boundary's planner emits per-instance subtrees for them.
func (e *Executor) planNodeSteps(rs *runState, n *Node) ([]*PlanStep, error) {
	if e.insideForEachComposite(n) {
		return nil, nil
	}
	if n.ForEach != nil {
		if n.IsComposite() {
			return e.planForEachComposite(rs, n)
		}
		switch n.Kind {
		case NodeResource:
			return e.planForEachResource(rs, n)
		case NodeAction:
			return e.planForEachAction(rs, n)
		case NodeData:
			return e.planForEachData(rs, n)
		}
	}
	step, err := e.planNode(rs, n)
	if err != nil {
		return nil, err
	}
	if step == nil {
		return nil, nil
	}
	return []*PlanStep{step}, nil
}

func (e *Executor) seedFromPriorState(rs *runState) error {
	if rs.prior == nil {
		return nil
	}
	for _, ent := range rs.prior.Entries {
		switch ent.Type {
		case state.EntryAction:
			scope, err := e.scopeForAddress(rs, ent.Address)
			if err != nil {
				return err
			}
			if scope == nil {
				continue
			}
			tmpl, instKey := splitInstanceAddress(ent.Address)
			_, alias, typeName, name, ok := parseAddress(tmpl)
			if !ok {
				continue
			}
			if instKey == "" {
				seedNested(scope.Actions, alias, typeName, name, ent.Outputs)
			} else {
				seedInstance(scope.Actions, alias, typeName, name, instKey, ent.Outputs)
			}
		case state.EntryLeaf:
			scope, err := e.scopeForAddress(rs, ent.Address)
			if err != nil {
				return err
			}
			if scope == nil {
				continue
			}
			tmpl, instKey := splitInstanceAddress(ent.Address)
			_, alias, typeName, name, ok := parseAddress(tmpl)
			if !ok {
				continue
			}
			if instKey == "" {
				seedNested(scope.Resources, alias, typeName, name, ent.Outputs)
			} else {
				seedInstance(scope.Resources, alias, typeName, name, instKey, ent.Outputs)
			}
		}
	}
	return nil
}

// seedStepAttrs makes a planned leaf node's attribute view visible to
// the nodes that follow it in the topological walk, so a body that
// reads an input of an upstream node resolves it at plan time rather
// than deferring to apply. The view is the node's known inputs laid
// under any prior outputs, matching the apply-time merge. Only inputs
// that are actually known are seeded: a field still waiting on an
// upstream is left out so a reader sees it as unknown-until-apply
// rather than a misleading nil. A composite boundary is opaque except
// its declared outputs and is left to finalizeComposite; a destroy
// seeds nothing.
func (e *Executor) seedStepAttrs(rs *runState, step *PlanStep) error {
	if step.Composite || step.Decision == DecisionDestroy {
		return nil
	}
	switch step.Kind {
	case NodeResource, NodeData, NodeAction:
	default:
		return nil
	}
	scope, err := e.scopeForAddress(rs, step.Address)
	if err != nil || scope == nil {
		return err
	}
	tmpl, instKey := splitInstanceAddress(step.Address)
	_, alias, typeName, name, ok := parseAddress(tmpl)
	if !ok {
		return nil
	}
	target := scopeMapForKind(scope, step.Kind)
	attrs := mergeAttrs(knownInputs(step), step.PriorOutputs)
	if instKey == "" {
		seedNested(target, alias, typeName, name, attrs)
	} else {
		seedInstance(target, alias, typeName, name, instKey, attrs)
	}
	return nil
}

// knownInputs returns the step's input fields whose value is settled at
// plan time, dropping any field still waiting on an upstream node. A
// pending field left in would seed a nil that a downstream reader would
// take for a real value; left out, the reader sees unknown-until-apply
// and resolves once the upstream runs.
func knownInputs(step *PlanStep) map[string]any {
	if len(step.UnresolvedInputs) == 0 {
		return step.Inputs
	}
	known := make(map[string]any, len(step.Inputs))
	for name, value := range step.Inputs {
		if _, pending := step.UnresolvedInputs[name]; pending {
			continue
		}
		known[name] = value
	}
	return known
}

// scopeForAddress returns the scope a state entry belongs to. Entries
// addressed inside a composite (their address contains `/`) seed the
// scope of their direct enclosing composite, which is everything up
// to the last `/`. The callsite may carry a `['key']` suffix when the
// composite has `@for-each`; ensureCompositeScope builds the
// per-instance scope from it. When a prior entry's composite has been
// removed from source, its boundary is not in the DAG, so there is no
// scope to seed and the entry is skipped (a nil scope tells the
// caller to move on).
func (e *Executor) scopeForAddress(rs *runState, addr string) (*EvalContext, error) {
	if i := strings.LastIndex(addr, "/"); i >= 0 {
		callSite := addr[:i]
		if _, ok := e.DAG.Nodes[templateAddress(callSite)]; !ok {
			return nil, nil
		}
		scope, err := e.ensureCompositeScope(rs, callSite)
		if err != nil {
			if errors.Is(err, ErrInstanceGone) {
				return nil, nil
			}
			return nil, err
		}
		return scope, nil
	}
	return rs.eval, nil
}

func seedNested(target map[string]any, alias, typeName, name string, value map[string]any) {
	aliasMap := getOrCreate(target, alias)
	typeMap := getOrCreate(aliasMap, typeName)
	typeMap[name] = value
}

// seedInstance writes one for-each instance's outputs into scope at
// `target[alias][type][name][key] = value`, so that an expression like
// `resource.<alias>.<type>.<name>['<key>'].<field>` resolves through
// ordinary dot-path navigation.
func seedInstance(target map[string]any, alias, typeName, name, key string, value map[string]any) {
	aliasMap := getOrCreate(target, alias)
	typeMap := getOrCreate(aliasMap, typeName)
	nameMap := getOrCreate(typeMap, name)
	nameMap[key] = value
}

// SplitInstanceAddress separates a `<template>['<key>']` address into
// its template part and the instance key. Non-instance addresses
// return unchanged with an empty key.
func SplitInstanceAddress(addr string) (template, key string) {
	return splitInstanceAddress(addr)
}

// splitInstanceAddress is the package-internal version used by Plan
// and ApplyPlan. It is also exposed via SplitInstanceAddress for the
// renderer in `pkg/runner`.
func splitInstanceAddress(addr string) (template, key string) {
	if !strings.HasSuffix(addr, "']") {
		return addr, ""
	}
	idx := strings.LastIndex(addr, "['")
	if idx < 0 {
		return addr, ""
	}
	return addr[:idx], addr[idx+2 : len(addr)-2]
}

// isUpstreamChange reports whether the named decision implies the
// referenced node's outputs may differ from what's in prior state.
func isUpstreamChange(d Decision) bool {
	switch d {
	case DecisionCreate, DecisionUpdate, DecisionReplace, DecisionDestroy, DecisionRerun:
		return true
	}
	return false
}

func (e *Executor) planNode(rs *runState, n *Node) (*PlanStep, error) {
	if n.IsComposite() {
		return e.planComposite(rs, n)
	}
	switch n.Kind {
	case NodeAction:
		return e.planAction(rs, n)
	case NodeResource:
		return e.planResource(rs, n)
	case NodeData:
		scope, err := e.scopeFor(rs, n)
		if err != nil {
			return nil, err
		}
		return e.planOneData(n, scope, n.Address)
	case NodeOutput:
		return &PlanStep{Address: n.Address, Kind: n.Kind, Decision: DecisionEval}, nil
	default:
		return nil, fmt.Errorf("unknown node kind %q", n.Kind)
	}
}

// checkStepConstraints validates a leaf node's evaluated args against its
// Go type's constraints, returning one error per violation prefixed with
// the node address. It runs only when every arg resolved at plan, so a
// forward-reference arg (unknown until apply) never causes a false
// violation; a missing optional arg evaluates to null in a predicate
// rather than erroring. Composite boundaries and types that declare no
// constraint contribute nothing.
func (e *Executor) checkStepConstraints(step *PlanStep) []error {
	if step.Composite {
		return nil
	}
	switch step.Kind {
	case NodeResource, NodeData, NodeAction:
	default:
		return nil
	}
	if len(step.UnresolvedInputs) > 0 {
		return nil
	}
	tmpl := templateAddress(step.Address)
	kind, alias, typeName, _, ok := parseAddress(tmpl)
	if !ok {
		return nil
	}
	lib, ok := e.librariesForAddress(tmpl)[alias]
	if !ok || lib == nil {
		return nil
	}
	specs := lib.Constraints[string(kind)+"."+typeName]
	if len(specs) == 0 {
		return nil
	}
	entries, perr := lang.ParseSpecs(specs)
	values := make(map[string]any, len(step.Inputs))
	maps.Copy(values, step.Inputs)
	eval := func(ex lang.Expr) (any, error) {
		v, err := Eval(ex, &EvalContext{Vars: values, MissingAsNull: true})
		if errors.Is(err, ErrEvalNotFound) {
			return nil, nil
		}
		return v, err
	}
	var out []error
	for _, er := range perr.Errors() {
		out = append(out, fmt.Errorf("%s: %v", step.Address, er))
	}
	entryErrs := lang.CheckConstraintEntries(entries, values, eval, lang.DisplayNodeRelative)
	for _, er := range entryErrs.Errors() {
		out = append(out, fmt.Errorf("%s: %v", step.Address, er))
	}
	return out
}

// checkCompositeConstraints validates a composite boundary's own
// `constraints:` block, returning one error per violation prefixed with
// the boundary address. A composite's constraints are its input contract,
// checked the same way the factory checks its own: against the inputs
// alone. The call site's args were evaluated when the boundary scope was
// built, so a forward-reference arg makes that fail earlier and never
// reaches here. A composite applies no defaults, so every declared input
// the call site left out is filled with null first; an unset input then
// reads as null in a predicate, the same as an unset optional input does
// for the factory. Nodes other than composites contribute nothing, as do
// composites with no scope or no constraints.
func (e *Executor) checkCompositeConstraints(rs *runState, step *PlanStep) []error {
	if !step.Composite {
		return nil
	}
	node, ok := e.DAG.Nodes[templateAddress(step.Address)]
	if !ok || node.CompositeBody == nil {
		return nil
	}
	arr, ok := topLevelMap(node.CompositeBody.Body)["constraints"].(*lang.ArrayLit)
	if !ok || len(arr.Elements) == 0 {
		return nil
	}
	scope, ok := rs.composites[step.Address]
	if !ok || scope == nil {
		return nil
	}
	values := make(map[string]any, len(scope.Vars))
	for name := range inputNames(node.CompositeBody) {
		values[name] = nil
	}
	maps.Copy(values, scope.Vars)
	eval := func(ex lang.Expr) (any, error) {
		ctx := &EvalContext{Vars: values, Libraries: node.Libraries, MissingAsNull: true}
		return Eval(ex, ctx)
	}
	var out []error
	for _, er := range lang.CheckConstraints(arr, values, eval, lang.DisplayNodeRelative).Errors() {
		out = append(out, fmt.Errorf("%s: %v", step.Address, er))
	}
	return out
}

// planComposite plans the composite boundary. The call site args are
// evaluated against the boundary's enclosing scope (root for top-level
// boundaries, the outer composite's scope for nested ones) by
// constructing the composite scope; its Vars are those evaluated args.
// The boundary itself does no CRUD: its decision is Eval and its
// outputs are computed at apply time after the internals run.
func (e *Executor) planComposite(rs *runState, n *Node) (*PlanStep, error) {
	scope, err := e.ensureCompositeScope(rs, n.Address)
	if err != nil {
		return nil, err
	}
	var prior *state.Entry
	if rs.prior != nil {
		prior = rs.prior.Find(n.Address)
	}
	var priorOut map[string]any
	if prior != nil {
		priorOut = prior.Outputs
	}
	return &PlanStep{
		Address:      n.Address,
		Kind:         n.Kind,
		Composite:    true,
		Decision:     DecisionEval,
		Inputs:       scope.Vars,
		PriorOutputs: priorOut,
	}, nil
}

func (e *Executor) planAction(rs *runState, n *Node) (*PlanStep, error) {
	lib, ok := e.librariesFor(n)[n.Alias]
	if !ok {
		return nil, fmt.Errorf("library %q is not imported", n.Alias)
	}
	if _, ok := lib.Actions[n.Type]; !ok {
		return nil, fmt.Errorf("library %s has no action %q", n.Alias, n.Type)
	}
	scope, err := e.scopeFor(rs, n)
	if err != nil {
		return nil, err
	}
	return e.planOneAction(rs, n, scope, n.Address)
}

// planOneAction plans a single action instance against the given scope
// and state address. Used both by the plain action path
// (scope == parent, addr == n.Address) and by the for-each path
// (scope has @each bound, addr has the `['<key>']` suffix).
func (e *Executor) planOneAction(
	rs *runState, n *Node, scope *EvalContext, addr string,
) (*PlanStep, error) {
	inputs, unresolved, err := planEvalBody(n.Body, scope)
	if err != nil {
		return nil, err
	}
	trigger, err := ComputeTrigger(n, inputs, scope)
	if err != nil {
		return nil, err
	}

	var prior *state.Entry
	if rs.prior != nil {
		prior = rs.prior.Find(addr)
	}
	dec := DecisionRerun
	var priorOut map[string]any
	if prior != nil {
		priorOut = prior.Outputs
	}
	if !trigger.AlwaysRerun && prior != nil && prior.TriggerHash != "" &&
		prior.TriggerHash == trigger.Hash {
		dec = DecisionSkip
	}
	return &PlanStep{
		Address:          addr,
		Kind:             n.Kind,
		Decision:         dec,
		Inputs:           inputs,
		UnresolvedInputs: unresolved,
		PriorOutputs:     priorOut,
		TriggerHash:      trigger.Hash,
	}, nil
}

func (e *Executor) planResource(rs *runState, n *Node) (*PlanStep, error) {
	lib, ok := e.librariesFor(n)[n.Alias]
	if !ok {
		return nil, fmt.Errorf("library %q is not imported", n.Alias)
	}
	rt, ok := lib.Resources[n.Type]
	if !ok {
		return nil, fmt.Errorf("library %s has no resource %q", n.Alias, n.Type)
	}
	scope, err := e.scopeFor(rs, n)
	if err != nil {
		return nil, err
	}
	return e.planOneResource(rs, n, rt, scope, n.Address)
}

// pendingRead is the per-resource read job queued by planOneResource
// during Plan's serial walk. runPendingReads fans them out across
// workers; finalizePendingReads then sets each step's Decision and
// ObservedOutputs from the result.
type pendingRead struct {
	step         *PlanStep
	rt           ResourceRegistration
	cfg          any
	inputs       map[string]any
	priorInputs  map[string]any
	priorOutputs map[string]any
	observed     map[string]any
	err          error
}

// planOneResource plans a single resource instance against the given
// scope and state address. Used both by the plain resource path
// (scope == parent, addr == n.Address) and by the for-each path
// (scope has @each bound, addr has the `['<key>']` suffix). When the
// resource has prior state, the Read is deferred onto rs.pendingReads
// for parallel execution; the returned step's Decision is left blank
// until finalizePendingReads runs.
func (e *Executor) planOneResource(
	rs *runState, n *Node, rt ResourceRegistration,
	scope *EvalContext, addr string,
) (*PlanStep, error) {
	inputs, unresolved, err := planEvalBody(n.Body, scope)
	if err != nil {
		return nil, err
	}
	var prior *state.Entry
	if rs.prior != nil {
		prior = rs.prior.Find(addr)
	}
	step := &PlanStep{
		Address:          addr,
		Kind:             n.Kind,
		Inputs:           inputs,
		UnresolvedInputs: unresolved,
	}
	if prior == nil {
		step.Decision = DecisionCreate
		return step, nil
	}
	priorOutputs, err := migrateOutputs(rt, prior.SchemaVersion, prior.Outputs)
	if err != nil {
		return nil, err
	}
	step.PriorOutputs = priorOutputs
	rs.pendingReads = append(rs.pendingReads, &pendingRead{
		step:         step,
		rt:           rt,
		cfg:          e.configFor(n),
		inputs:       inputs,
		priorInputs:  prior.Inputs,
		priorOutputs: priorOutputs,
	})
	return step, nil
}

// runPendingReads runs every queued resource Read concurrently, up to
// effectiveParallelism. The result is stashed on each pendingRead so
// finalizePendingReads can pick between ErrNotFound (which maps to
// Create) and a real read failure (which propagates).
func (e *Executor) runPendingReads(ctx context.Context, rs *runState) error {
	if len(rs.pendingReads) == 0 {
		return nil
	}
	sem := make(chan struct{}, e.effectiveParallelism())
	var wg sync.WaitGroup
	for _, pr := range rs.pendingReads {
		wg.Add(1)
		sem <- struct{}{}
		go func(pr *pendingRead) {
			defer func() { <-sem; wg.Done() }()
			pr.observed, pr.err = readObserved(ctx, pr.rt, pr.cfg, pr.inputs, pr.priorOutputs)
		}(pr)
	}
	wg.Wait()
	return nil
}

// finalizePendingReads walks the queued reads in plan order and turns
// each (observed, err) pair into a step Decision: ErrNotFound becomes
// Create, any other error propagates, and a clean read picks between
// Replace, Update, and NoOp using the existing input-diff and
// drift-diff rules.
func (e *Executor) finalizePendingReads(rs *runState) error {
	for _, pr := range rs.pendingReads {
		if err := finalizeResourceRead(pr); err != nil {
			return fmt.Errorf("%s: %w", pr.step.Address, err)
		}
	}
	return nil
}

func finalizeResourceRead(pr *pendingRead) error {
	if errors.Is(pr.err, ErrNotFound) {
		pr.step.Decision = DecisionCreate
		return nil
	}
	if pr.err != nil {
		return fmt.Errorf("read: %w", pr.err)
	}
	pr.step.ObservedOutputs = pr.observed
	if !sameInputs(pr.priorInputs, pr.inputs) {
		probe := pr.rt.NewReceiver()
		if err := Decode(probe, pr.inputs); err != nil {
			return err
		}
		if needsReplace(pr.rt.ReplaceFields(probe), pr.priorInputs, pr.inputs) {
			pr.step.Decision = DecisionReplace
		} else {
			pr.step.Decision = DecisionUpdate
		}
		return nil
	}
	if pr.step.Drift() {
		pr.step.Decision = DecisionUpdate
		return nil
	}
	pr.step.Decision = DecisionNoOp
	return nil
}

// upgradeActionRerun walks plan steps in order and turns a Skip action
// into a Rerun when any of its references points to a step whose
// decision implies an upstream change. A reference made through a local
// counts: the local is followed to the upstream it reads. The walk runs
// after all reads have finalized, so every step's Decision is set; it
// updates the per-address index in place so a transitive Skip->Rerun
// chain picks up the upgrade from an earlier step.
func upgradeActionRerun(steps []*PlanStep, dag *DAG, sl *scopeLocals) {
	addressDecision := make(map[string]Decision, len(steps))
	for _, step := range steps {
		addressDecision[step.Address] = step.Decision
	}
	for _, step := range steps {
		if step.Kind != NodeAction || step.Decision != DecisionSkip {
			continue
		}
		node := dag.Nodes[templateAddress(step.Address)]
		if node == nil {
			continue
		}
		for _, ref := range refsWithLocals(node.Body, sl.forScope(node.Composite)) {
			if isUpstreamChange(addressDecision[ref]) {
				step.Decision = DecisionRerun
				step.TriggerHash = ""
				addressDecision[step.Address] = DecisionRerun
				break
			}
		}
	}
}

// planOneData plans a single data source instance against the given
// scope and state address.
func (e *Executor) planOneData(n *Node, scope *EvalContext, addr string) (*PlanStep, error) {
	inputs, unresolved, err := planEvalBody(n.Body, scope)
	if err != nil {
		return nil, err
	}
	return &PlanStep{
		Address:          addr,
		Kind:             n.Kind,
		Decision:         DecisionRead,
		Inputs:           inputs,
		UnresolvedInputs: unresolved,
	}, nil
}

// readObserved decodes inputs onto a fresh resource and asks the
// library what's in the cloud for it. It returns the result in the
// same canonical map state uses, or ErrNotFound when the resource is
// gone.
func readObserved(
	ctx context.Context,
	rt ResourceRegistration,
	cfg any,
	inputs, priorOutputs map[string]any,
) (map[string]any, error) {
	receiver := rt.NewReceiver()
	if err := Decode(receiver, inputs); err != nil {
		return nil, err
	}
	result, err := rt.Read(ctx, receiver, cfg, priorOutputs)
	if err != nil {
		return nil, err
	}
	return mapify(result), nil
}
