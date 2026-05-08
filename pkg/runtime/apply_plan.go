package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/state"
)

// ApplyPlan executes a previously computed PlanFile against the
// Executor's modules, store, and parsed source. The DAG passed on the
// Executor is used for output expressions, while resource and action
// bodies come from the plan. The plan's stack identity must match the
// Executor's, and the prior state's rev must match what the plan was
// computed against. The deployment's lock is held for the duration.
func (e *Executor) ApplyPlan(ctx context.Context, pf *PlanFile) (*ExecResult, error) {
	if e.Store == nil {
		return nil, errors.New("executor: Store is required")
	}
	if pf.Stack.Name != e.Stack.Name ||
		pf.Stack.Version != e.Stack.Version ||
		pf.Stack.Commit != e.Stack.Commit {
		return nil, fmt.Errorf(
			"plan was computed for %s %s (commit %s), this binary is %s %s (commit %s)",
			pf.Stack.Name, pf.Stack.Version, pf.Stack.Commit,
			e.Stack.Name, e.Stack.Version, e.Stack.Commit)
	}

	lock, err := e.Store.Lock(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	currentRev, _ := e.Store.CurrentRev()
	if currentRev != pf.StateRev {
		return nil, fmt.Errorf("state-rev drift: plan was computed against %q, "+
			"current is %q; must rerun the plan", pf.StateRev, currentRev)
	}

	rs, err := e.initRun()
	if err != nil {
		return nil, err
	}

	// Composite scopes seed from the plan: each composite step carries
	// its evaluated call site args as Inputs, so internals see the
	// right Vars without needing the root inputs again.
	for i := range pf.Steps {
		step := &pf.Steps[i]
		if step.Kind != NodeComposite {
			continue
		}
		rs.composites[step.Address] = &EvalContext{
			Vars:      step.Inputs,
			Resources: make(map[string]any),
			Data:      make(map[string]any),
			Actions:   make(map[string]any),
		}
	}

	for i := range pf.Steps {
		step := &pf.Steps[i]
		if err := e.applyStep(ctx, rs, step); err != nil {
			return nil, fmt.Errorf("%s: %w", step.Address, err)
		}
	}
	if err := e.evalPlanOutputs(rs); err != nil {
		return nil, err
	}
	rs.next.Outputs = rs.outputs

	rev, err := e.persist(rs)
	if err != nil {
		return nil, err
	}
	return &ExecResult{
		Outputs:    rs.outputs,
		Actions:    rs.eval.Actions,
		Data:       rs.eval.Data,
		WrittenRev: rev,
	}, nil
}

func (e *Executor) applyStep(ctx context.Context, rs *runState, step *PlanStep) error {
	switch step.Kind {
	case NodeAction:
		return e.applyAction(ctx, rs, step)
	case NodeResource:
		return e.applyResource(ctx, rs, step)
	case NodeData:
		return e.applyData(ctx, rs, step)
	case NodeComposite:
		node, ok := e.DAG.Nodes[step.Address]
		if !ok || node.Kind != NodeComposite {
			return fmt.Errorf("composite: node %q not in DAG", step.Address)
		}
		return e.finalizeComposite(rs, node, step.Inputs)
	case NodeOutput:
		return nil
	default:
		return fmt.Errorf("unknown step kind %q", step.Kind)
	}
}

func (e *Executor) applyAction(ctx context.Context, rs *runState, step *PlanStep) error {
	node, scope, err := e.nodeAndScope(rs, step.Address)
	if err != nil {
		return err
	}
	mod, ok := e.Modules[node.NS]
	if !ok {
		return fmt.Errorf("module %q is not imported", node.NS)
	}
	at, ok := mod.Actions[node.Type]
	if !ok {
		return fmt.Errorf("module %s has no action %q", node.NS, node.Type)
	}
	var outputs map[string]any
	switch step.Decision {
	case DecisionSkip:
		outputs = step.PriorOutputs
	case DecisionRerun:
		action := at.New()
		if err := Decode(action, step.Inputs); err != nil {
			return err
		}
		result, err := action.Run(ctx)
		if err != nil {
			return err
		}
		outputs = mapify(result)
	default:
		return fmt.Errorf("action: unexpected decision %q", step.Decision)
	}
	storeNested(scope.Actions, node, outputs)

	// Recompute the trigger hash with the fresh upstream state so the
	// next plan compares against an accurate hash.
	hash := step.TriggerHash
	if t, err := ComputeTrigger(node, step.Inputs, scope); err == nil && !t.AlwaysRerun {
		hash = t.Hash
	}

	rs.next.Entries = append(rs.next.Entries, &state.Entry{
		Address:     step.Address,
		Type:        state.EntryAction,
		Kind:        node.Type,
		TriggerHash: hash,
		Inputs:      step.Inputs,
		Outputs:     outputs,
	})
	return nil
}

func (e *Executor) applyResource(ctx context.Context, rs *runState, step *PlanStep) error {
	if step.Decision == DecisionDestroy {
		return e.applyDestroy(ctx, step)
	}
	node, scope, err := e.nodeAndScope(rs, step.Address)
	if err != nil {
		return err
	}
	mod, ok := e.Modules[node.NS]
	if !ok {
		return fmt.Errorf("module %q is not imported", node.NS)
	}
	rt, ok := mod.Resources[node.Type]
	if !ok {
		return fmt.Errorf("module %s has no resource %q", node.NS, node.Type)
	}

	resource := rt.New()
	if err := Decode(resource, step.Inputs); err != nil {
		return err
	}
	var outputs map[string]any
	switch step.Decision {
	case DecisionCreate:
		result, err := resource.Create(ctx)
		if err != nil {
			return err
		}
		outputs = mapify(result)
	case DecisionNoOp:
		outputs = step.PriorOutputs
	case DecisionUpdate:
		result, err := resource.Update(ctx, step.PriorOutputs)
		if err != nil {
			return err
		}
		outputs = mapify(result)
	case DecisionReplace:
		if err := resource.Delete(ctx, step.PriorOutputs); err != nil {
			return fmt.Errorf("replace: delete prior: %w", err)
		}
		result, err := resource.Create(ctx)
		if err != nil {
			return fmt.Errorf("replace: create: %w", err)
		}
		outputs = mapify(result)
	default:
		return fmt.Errorf("resource: unexpected decision %q", step.Decision)
	}
	storeNested(scope.Resources, node, outputs)
	rs.next.Entries = append(rs.next.Entries, &state.Entry{
		Address:       step.Address,
		Type:          state.EntryLeaf,
		Kind:          node.Type,
		SchemaVersion: rt.SchemaVersion,
		Inputs:        step.Inputs,
		Outputs:       outputs,
	})
	return nil
}

// applyDestroy handles an orphan destroy step. The address is not in
// the DAG (since the node was removed from source), so the resource
// type is recovered by parsing the address. Composite-internal
// addresses keep their call site prefix, so the inner address is
// stripped first.
func (e *Executor) applyDestroy(ctx context.Context, step *PlanStep) error {
	ns, typeName, _, ok := parseResourceAddress(innerAddress(step.Address))
	if !ok {
		return fmt.Errorf("destroy: malformed address %q", step.Address)
	}
	mod, ok := e.Modules[ns]
	if !ok {
		return fmt.Errorf("module %q is not imported", ns)
	}
	rt, ok := mod.Resources[typeName]
	if !ok {
		return fmt.Errorf("module %s has no resource %q", ns, typeName)
	}
	resource := rt.New()
	if err := Decode(resource, step.Inputs); err != nil {
		return err
	}
	return resource.Delete(ctx, step.PriorOutputs)
}

// nodeAndScope looks up the DAG node at addr and returns it together
// with the EvalContext for evaluating its body. Composite internals
// resolve to the composite's own scope; root nodes resolve to root.
func (e *Executor) nodeAndScope(rs *runState, addr string) (*Node, *EvalContext, error) {
	node, ok := e.DAG.Nodes[addr]
	if !ok {
		return nil, nil, fmt.Errorf("address %q not in DAG", addr)
	}
	scope, err := e.scopeFor(rs, node)
	if err != nil {
		return nil, nil, err
	}
	return node, scope, nil
}

func (e *Executor) applyData(ctx context.Context, rs *runState, step *PlanStep) error {
	node, scope, err := e.nodeAndScope(rs, step.Address)
	if err != nil {
		return err
	}
	mod, ok := e.Modules[node.NS]
	if !ok {
		return fmt.Errorf("module %q is not imported", node.NS)
	}
	dt, ok := mod.DataSources[node.Type]
	if !ok {
		return fmt.Errorf("module %s has no data source %q", node.NS, node.Type)
	}
	ds := dt.New()
	if err := Decode(ds, step.Inputs); err != nil {
		return err
	}
	result, err := ds.Read(ctx)
	if err != nil {
		return err
	}
	storeNested(scope.Data, node, mapify(result))
	return nil
}

// evalPlanOutputs evaluates each output node from the source against the
// runtime context built up while applying the plan.
func (e *Executor) evalPlanOutputs(rs *runState) error {
	for _, n := range e.DAG.Nodes {
		if n.Kind != NodeOutput {
			continue
		}
		val, err := Eval(n.Body, rs.eval)
		if err != nil {
			return fmt.Errorf("%s: %w", n.Address, err)
		}
		rs.outputs[n.Name] = val
	}
	return nil
}
