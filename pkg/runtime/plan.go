package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/state"
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
		refs := deferredRefs(fld.Value)
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
	Address          string              `json:"address"`
	Kind             NodeKind            `json:"kind"`
	Decision         Decision            `json:"decision"`
	Inputs           map[string]any      `json:"inputs,omitempty"`
	UnresolvedInputs map[string][]string `json:"unresolved-inputs,omitempty"`
	PriorOutputs     map[string]any      `json:"prior-outputs,omitempty"`
	ObservedOutputs  map[string]any      `json:"observed-outputs,omitempty"`
	TriggerHash      string              `json:"trigger-hash,omitempty"`
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
// the raw per-module configuration maps (keyed by import alias then
// alias name) so apply re-decodes them through the same code path
// rather than re-reading the config file.
type Plan struct {
	Stack              state.StackInfo
	DeploymentID       string
	StateRev           string
	Inputs             map[string]any
	RawConfigurations  map[string]map[string]any
	Steps              []*PlanStep
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
		Stack:        e.Stack,
		DeploymentID: e.Store.DeploymentID(),
		StateRev:     stateRev,
		Inputs:       e.Inputs,
	}

	// Seed the EvalContext with prior outputs so downstream evaluation
	// has something to bind to even when an upstream node would change.
	if err := e.seedFromPriorState(rs); err != nil {
		return nil, err
	}

	addressDecision := make(map[string]Decision)
	liveAddresses := make(map[string]bool)
	for _, addr := range order {
		node := e.DAG.Nodes[addr]
		steps, err := e.planNodeSteps(ctx, rs, node)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", addr, err)
		}
		for _, step := range steps {
			// An action whose upstream is changing must rerun, even when
			// its own inputs and trigger value (against prior state) match.
			if step.Kind == NodeAction && step.Decision == DecisionSkip {
				for _, ref := range Refs(node.Body) {
					if isUpstreamChange(addressDecision[ref]) {
						step.Decision = DecisionRerun
						step.TriggerHash = ""
						break
					}
				}
			}
			plan.Steps = append(plan.Steps, step)
			addressDecision[step.Address] = step.Decision
			liveAddresses[step.Address] = true
		}
	}

	// Orphans: prior leaf entries with no live address in this plan.
	if rs.prior != nil {
		for _, prior := range rs.prior.Entries {
			if prior.Type != state.EntryLeaf {
				continue
			}
			if liveAddresses[prior.Address] {
				continue
			}
			plan.Steps = append(plan.Steps, &PlanStep{
				Address:      prior.Address,
				Kind:         NodeResource,
				Decision:     DecisionDestroy,
				Inputs:       prior.Inputs,
				PriorOutputs: prior.Outputs,
			})
		}
	}
	return plan, nil
}

// planNodeSteps wraps planNode so a single per-node planner can emit
// multiple steps. A `@for-each` template (resource, action, data
// source, or composite) fans out into one step per instance;
// everything else returns a one-element slice (or nil to skip the
// node). Nodes inside a `@for-each` composite are skipped here: their
// boundary's planner emits per-instance subtrees for them.
func (e *Executor) planNodeSteps(ctx context.Context, rs *runState, n *Node) ([]*PlanStep, error) {
	if e.insideForEachComposite(n) {
		return nil, nil
	}
	if n.ForEach != nil {
		switch n.Kind {
		case NodeResource:
			return e.planForEachResource(ctx, rs, n)
		case NodeAction:
			return e.planForEachAction(rs, n)
		case NodeData:
			return e.planForEachData(rs, n)
		case NodeComposite:
			return e.planForEachComposite(ctx, rs, n)
		}
	}
	step, err := e.planNode(ctx, rs, n)
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
			ns, kind, name, ok := parseActionAddress(innerAddress(tmpl))
			if !ok {
				continue
			}
			if instKey == "" {
				seedNested(scope.Actions, ns, kind, name, ent.Outputs)
			} else {
				seedInstance(scope.Actions, ns, kind, name, instKey, ent.Outputs)
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
			ns, typeName, name, ok := parseResourceAddress(innerAddress(tmpl))
			if !ok {
				continue
			}
			if instKey == "" {
				seedNested(scope.Resources, ns, typeName, name, ent.Outputs)
			} else {
				seedInstance(scope.Resources, ns, typeName, name, instKey, ent.Outputs)
			}
		}
	}
	return nil
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

// innerAddress strips the call site prefix from a composite-internal
// address so the existing parsers can read it. A root address comes
// back unchanged.
func innerAddress(addr string) string {
	if i := strings.Index(addr, "/"); i >= 0 {
		// Internal addresses drop the leading "resource." from the
		// inner part for resources. Restore it so parseResourceAddress
		// keeps working.
		inner := addr[i+1:]
		if !strings.HasPrefix(inner, "data.") &&
			!strings.HasPrefix(inner, "action.") &&
			!strings.HasPrefix(inner, "resource.") {
			return "resource." + inner
		}
		return inner
	}
	return addr
}

func seedNested(target map[string]any, ns, typeName, name string, value map[string]any) {
	nsMap := getOrCreate(target, ns)
	typeMap := getOrCreate(nsMap, typeName)
	typeMap[name] = value
}

// seedInstance writes one for-each instance's outputs into scope at
// `target[ns][type][name][key] = value`, so that an expression like
// `resource.<ns>.<type>.<name>['<key>'].<field>` resolves through
// ordinary dot-path navigation.
func seedInstance(target map[string]any, ns, typeName, name, key string, value map[string]any) {
	nsMap := getOrCreate(target, ns)
	typeMap := getOrCreate(nsMap, typeName)
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

func parseActionAddress(addr string) (ns, kind, name string, ok bool) {
	parts := strings.SplitN(addr, ".", 4)
	if len(parts) != 4 || parts[0] != "action" {
		return "", "", "", false
	}
	return parts[1], parts[2], parts[3], true
}

func (e *Executor) planNode(ctx context.Context, rs *runState, n *Node) (*PlanStep, error) {
	switch n.Kind {
	case NodeAction:
		return e.planAction(rs, n)
	case NodeResource:
		return e.planResource(ctx, rs, n)
	case NodeComposite:
		return e.planComposite(rs, n)
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
		Decision:     DecisionEval,
		Inputs:       scope.Vars,
		PriorOutputs: priorOut,
	}, nil
}

func (e *Executor) planAction(rs *runState, n *Node) (*PlanStep, error) {
	mod, ok := e.modulesFor(n)[n.NS]
	if !ok {
		return nil, fmt.Errorf("module %q is not imported", n.NS)
	}
	if _, ok := mod.Actions[n.Type]; !ok {
		return nil, fmt.Errorf("module %s has no action %q", n.NS, n.Type)
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

func (e *Executor) planResource(ctx context.Context, rs *runState, n *Node) (*PlanStep, error) {
	mod, ok := e.modulesFor(n)[n.NS]
	if !ok {
		return nil, fmt.Errorf("module %q is not imported", n.NS)
	}
	rt, ok := mod.Resources[n.Type]
	if !ok {
		return nil, fmt.Errorf("module %s has no resource %q", n.NS, n.Type)
	}
	scope, err := e.scopeFor(rs, n)
	if err != nil {
		return nil, err
	}
	return e.planOneResource(ctx, rs, n, rt, scope, n.Address)
}

// planOneResource plans a single resource instance against the given
// scope and state address. Used both by the plain resource path
// (scope == parent, addr == n.Address) and by the for-each path
// (scope has @each bound, addr has the `['<key>']` suffix).
func (e *Executor) planOneResource(
	ctx context.Context, rs *runState, n *Node, rt ResourceType,
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

	observed, err := readObserved(ctx, rt, e.configFor(n), inputs, priorOutputs)
	if errors.Is(err, ErrNotFound) {
		step.Decision = DecisionCreate
		return step, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	step.ObservedOutputs = observed

	if !sameInputs(prior.Inputs, inputs) {
		probe := rt.New()
		if err := Decode(probe, inputs); err != nil {
			return nil, err
		}
		if needsReplace(probe, prior.Inputs, inputs) {
			step.Decision = DecisionReplace
		} else {
			step.Decision = DecisionUpdate
		}
		return step, nil
	}
	if step.Drift() {
		step.Decision = DecisionUpdate
		return step, nil
	}
	step.Decision = DecisionNoOp
	return step, nil
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
// module what's in the cloud for it. It returns the result in the
// same canonical map state uses, or ErrNotFound when the resource is
// gone.
func readObserved(
	ctx context.Context,
	rt ResourceType,
	cfg any,
	inputs, priorOutputs map[string]any,
) (map[string]any, error) {
	res := rt.New()
	if err := Decode(res, inputs); err != nil {
		return nil, err
	}
	result, err := res.Read(ctx, cfg, priorOutputs)
	if err != nil {
		return nil, err
	}
	return mapify(result), nil
}
