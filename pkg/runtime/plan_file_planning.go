package runtime

import (
	"context"
	"fmt"
	"time"
)

type planFileV2Request struct {
	Factory       FactoryRef
	Stack         string
	StateRevision string
	GeneratedAt   time.Time
	Inputs        EncodedValue
	Backend       *StateRefV2
	Parallelism   int
	Mode          PlanMode
	StateMoves    []PlannedEntryMove
	Evaluate      func(*planningPassState) ([]planStepV2Request, error)
}

func planPlanFileV2(
	ctx context.Context,
	request planFileV2Request,
) (PlanFileV2, error) {
	if ctx == nil {
		return PlanFileV2{}, fmt.Errorf("planning context is required")
	}
	if request.Evaluate == nil {
		return PlanFileV2{}, fmt.Errorf("plan step evaluator is required")
	}

	plan, err := preparePlanFileV2(request)
	if err != nil {
		return PlanFileV2{}, err
	}

	steps, err := planStepsV2(ctx, request.Evaluate)
	if err != nil {
		return PlanFileV2{}, err
	}
	plan.Steps = steps
	plan, err = finalizePlanFileV2(plan)
	if err != nil {
		return PlanFileV2{}, fmt.Errorf("finalize version 2 plan: %w", err)
	}
	return plan, nil
}

func preparePlanFileV2(request planFileV2Request) (PlanFileV2, error) {
	plan, err := finalizePlanFileV2(PlanFileV2{
		Factory:       request.Factory,
		Stack:         request.Stack,
		StateRevision: request.StateRevision,
		GeneratedAt:   request.GeneratedAt,
		Inputs:        request.Inputs,
		Backend:       request.Backend,
		Parallelism:   request.Parallelism,
		Mode:          request.Mode,
		StateMoves:    request.StateMoves,
		Steps:         []PlanStepV2{},
	})
	if err != nil {
		return PlanFileV2{}, fmt.Errorf("prepare version 2 plan: %w", err)
	}
	return plan, nil
}
