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
	case NodeOutput:
		return nil
	default:
		return fmt.Errorf("unknown step kind %q", step.Kind)
	}
}

func (e *Executor) applyAction(ctx context.Context, rs *runState, step *PlanStep) error {
	ns, kind, name, ok := parseActionAddress(step.Address)
	if !ok {
		return fmt.Errorf("malformed address")
	}
	mod, ok := e.Modules[ns]
	if !ok {
		return fmt.Errorf("module %q is not imported", ns)
	}
	at, ok := mod.Actions[kind]
	if !ok {
		return fmt.Errorf("module %s has no action %q", ns, kind)
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
	storeNested(rs.eval.Actions, &Node{NS: ns, Type: kind, Name: name}, outputs)

	// Recompute the trigger hash with the fresh upstream state so the
	// next plan compares against an accurate hash.
	hash := step.TriggerHash
	if node, ok := e.DAG.Nodes[step.Address]; ok {
		if t, err := ComputeTrigger(node, step.Inputs, rs.eval); err == nil && !t.AlwaysRerun {
			hash = t.Hash
		}
	}

	rs.next.Entries = append(rs.next.Entries, &state.Entry{
		Address:     step.Address,
		Type:        state.EntryAction,
		Kind:        kind,
		TriggerHash: hash,
		Inputs:      step.Inputs,
		Outputs:     outputs,
	})
	return nil
}

func (e *Executor) applyResource(ctx context.Context, rs *runState, step *PlanStep) error {
	ns, typeName, name, ok := parseResourceAddress(step.Address)
	if !ok {
		return fmt.Errorf("malformed address")
	}
	mod, ok := e.Modules[ns]
	if !ok {
		return fmt.Errorf("module %q is not imported", ns)
	}
	rt, ok := mod.Resources[typeName]
	if !ok {
		return fmt.Errorf("module %s has no resource %q", ns, typeName)
	}

	if step.Decision == DecisionDestroy {
		resource := rt.New()
		if err := Decode(resource, step.Inputs); err != nil {
			return err
		}
		return resource.Delete(ctx, step.PriorOutputs)
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
	storeNested(rs.eval.Resources, &Node{NS: ns, Type: typeName, Name: name}, outputs)
	rs.next.Entries = append(rs.next.Entries, &state.Entry{
		Address:       step.Address,
		Type:          state.EntryLeaf,
		Kind:          typeName,
		SchemaVersion: rt.SchemaVersion,
		Inputs:        step.Inputs,
		Outputs:       outputs,
	})
	return nil
}

func (e *Executor) applyData(ctx context.Context, rs *runState, step *PlanStep) error {
	ns := splitFirst(step.Address, ".")[1]
	typeName := splitFirst(step.Address, ".")[2]
	name := splitFirst(step.Address, ".")[3]
	mod, ok := e.Modules[ns]
	if !ok {
		return fmt.Errorf("module %q is not imported", ns)
	}
	dt, ok := mod.DataSources[typeName]
	if !ok {
		return fmt.Errorf("module %s has no data source %q", ns, typeName)
	}
	ds := dt.New()
	if err := Decode(ds, step.Inputs); err != nil {
		return err
	}
	result, err := ds.Read(ctx)
	if err != nil {
		return err
	}
	storeNested(rs.eval.Data, &Node{NS: ns, Type: typeName, Name: name}, mapify(result))
	return nil
}

func splitFirst(s, sep string) []string {
	out := []string{}
	cur := ""
	for _, c := range s {
		if string(c) == sep {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	for len(out) < 4 {
		out = append(out, "")
	}
	return out
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
