package runtime

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/cloudboss/unobin/pkg/typecheck"
)

func (e *Executor) PlanV2(ctx context.Context) (*PlanFileV2, error) {
	if ctx == nil {
		return nil, fmt.Errorf("planning context is required")
	}
	if e == nil || e.DAG == nil {
		return nil, fmt.Errorf("executor dependency graph is required")
	}
	if e.LibraryCatalog == nil {
		return nil, fmt.Errorf("factory library catalog is required")
	}
	if e.Store == nil {
		return nil, fmt.Errorf("state store is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshots, err := planFileV2PlanningSnapshots(e.Store)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]EncodedValue, len(e.Inputs))
	for _, name := range slices.Sorted(maps.Keys(e.Inputs)) {
		fields[name], err = encodePlanningValue(typecheck.TUnknown(), e.Inputs[name])
		if err != nil {
			return nil, fmt.Errorf("input %q: %w", name, err)
		}
	}
	inputs, err := ObjectValue(fields)
	if err != nil {
		return nil, fmt.Errorf("planning inputs: %w", err)
	}
	revision, err := checkedCurrentRevision(e.Store)
	if err != nil {
		return nil, err
	}
	stack := e.Store.Stack()
	parallelism := e.Parallelism
	if parallelism <= 0 {
		parallelism = DefaultParallelism
	}
	mode := PlanApply
	if e.Destroy {
		mode = PlanDestroy
	}
	plan, err := preparePlanFileV2(planFileV2Request{
		Factory: FactoryRef{
			Name: e.Factory.Name, Version: e.Factory.Version, ContentRevision: e.Factory.ContentRevision,
		},
		Stack: stack, StateRevision: revision, GeneratedAt: time.Now().UTC(),
		Inputs: inputs, Parallelism: parallelism, Mode: mode, StateMoves: []PlannedEntryMove{},
	})
	if err != nil {
		return nil, err
	}
	snapshot, err := preparePlanFileV2State(ctx, planFileV2PlanningStart{
		Factory: e.Factory, Stack: stack, StateRevision: revision,
	}, snapshots)
	if err != nil {
		return nil, err
	}
	snapshot, plan.StateMoves, err = e.planSourceEntryMovesV2(snapshot)
	if err != nil {
		return nil, fmt.Errorf("source state moves: %w", err)
	}
	plan.Steps, err = e.planFactoryStepsV2(ctx, inputs, snapshot)
	if err != nil {
		return nil, err
	}
	plan, err = finalizePlanFileV2(plan)
	if err != nil {
		return nil, err
	}
	return &plan, nil
}
