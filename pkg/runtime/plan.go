package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/state"
)

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
type PlanStep struct {
	Address         string         `json:"address"`
	Kind            NodeKind       `json:"kind"`
	Decision        Decision       `json:"decision"`
	Inputs          map[string]any `json:"inputs,omitempty"`
	PriorOutputs    map[string]any `json:"prior-outputs,omitempty"`
	ObservedOutputs map[string]any `json:"observed-outputs,omitempty"`
	TriggerHash     string         `json:"trigger-hash,omitempty"`
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
// rejects the plan when the current rev no longer matches.
type Plan struct {
	Stack        state.StackInfo
	DeploymentID string
	StateRev     string
	Steps        []*PlanStep
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
	}

	// Seed the EvalContext with prior outputs so downstream evaluation
	// has something to bind to even when an upstream node would change.
	if err := e.seedFromPriorState(rs); err != nil {
		return nil, err
	}

	addressDecision := make(map[string]Decision)
	for _, addr := range order {
		node := e.DAG.Nodes[addr]
		step, err := e.planNode(ctx, rs, node)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", addr, err)
		}
		if step == nil {
			continue
		}
		// An action whose upstream is changing must rerun, even when its
		// own inputs and trigger value (against prior state) match.
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
	}

	// Orphans: prior leaf entries with no source resource node.
	if rs.prior != nil {
		for _, prior := range rs.prior.Entries {
			if prior.Type != state.EntryLeaf {
				continue
			}
			if _, ok := e.DAG.Nodes[prior.Address]; ok {
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
			ns, kind, name, ok := parseActionAddress(innerAddress(ent.Address))
			if !ok {
				continue
			}
			seedNested(scope.Actions, ns, kind, name, ent.Outputs)
		case state.EntryLeaf:
			scope, err := e.scopeForAddress(rs, ent.Address)
			if err != nil {
				return err
			}
			if scope == nil {
				continue
			}
			ns, typeName, name, ok := parseResourceAddress(innerAddress(ent.Address))
			if !ok {
				continue
			}
			seedNested(scope.Resources, ns, typeName, name, ent.Outputs)
		}
	}
	return nil
}

// scopeForAddress returns the scope a state entry belongs to. Entries
// addressed inside a composite (their address contains `/`) seed the
// scope of their direct enclosing composite, which is everything up
// to the last `/` for nested composites. When a prior entry's
// composite has been removed from source, its boundary is not in the
// DAG, so there is no scope to seed and the entry is skipped (a nil
// scope tells the caller to move on).
func (e *Executor) scopeForAddress(rs *runState, addr string) (*EvalContext, error) {
	if i := strings.LastIndex(addr, "/"); i >= 0 {
		callSite := addr[:i]
		if _, ok := e.DAG.Nodes[callSite]; !ok {
			return nil, nil
		}
		return e.ensureCompositeScope(rs, callSite)
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
		inputs, err := evalBody(n.Body, scope)
		if err != nil {
			return nil, err
		}
		return &PlanStep{
			Address:  n.Address,
			Kind:     n.Kind,
			Decision: DecisionRead,
			Inputs:   inputs,
		}, nil
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
	mod, ok := e.Modules[n.NS]
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
	inputs, err := evalBody(n.Body, scope)
	if err != nil {
		return nil, err
	}
	trigger, err := ComputeTrigger(n, inputs, scope)
	if err != nil {
		return nil, err
	}

	var prior *state.Entry
	if rs.prior != nil {
		prior = rs.prior.Find(n.Address)
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
		Address:      n.Address,
		Kind:         n.Kind,
		Decision:     dec,
		Inputs:       inputs,
		PriorOutputs: priorOut,
		TriggerHash:  trigger.Hash,
	}, nil
}

func (e *Executor) planResource(ctx context.Context, rs *runState, n *Node) (*PlanStep, error) {
	mod, ok := e.Modules[n.NS]
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
	inputs, err := evalBody(n.Body, scope)
	if err != nil {
		return nil, err
	}

	var prior *state.Entry
	if rs.prior != nil {
		prior = rs.prior.Find(n.Address)
	}
	step := &PlanStep{
		Address: n.Address,
		Kind:    n.Kind,
		Inputs:  inputs,
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

	observed, err := readObserved(ctx, rt, inputs, priorOutputs)
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

// readObserved decodes inputs onto a fresh resource and asks the
// module what's in the cloud for it. It returns the result in the same
// canonical map shape state uses, or ErrNotFound when the resource is
// gone.
func readObserved(
	ctx context.Context,
	rt ResourceType,
	inputs, priorOutputs map[string]any,
) (map[string]any, error) {
	res := rt.New()
	if err := Decode(res, inputs); err != nil {
		return nil, err
	}
	result, err := res.Read(ctx, priorOutputs)
	if err != nil {
		return nil, err
	}
	return mapify(result), nil
}
