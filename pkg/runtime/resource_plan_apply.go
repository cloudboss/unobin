package runtime

import (
	"context"
	"fmt"
)

type registeredResourceStepEvaluator func(
	context.Context,
	PlanStepV2,
) (registeredResourceSnapshotApplyRequest, error)

func applyRegisteredResourceSteps(
	ctx context.Context,
	applyState *applyStateV2,
	steps []PlanStepV2,
	parallelism int,
	evaluate registeredResourceStepEvaluator,
) error {
	if ctx == nil {
		return fmt.Errorf("apply context is required")
	}
	if applyState == nil {
		return fmt.Errorf("version 2 apply state is required")
	}
	if evaluate == nil {
		return fmt.Errorf("resource apply evaluator is required")
	}
	for i := range steps {
		if steps[i].Kind != NodeResource ||
			steps[i].Operation.Kind != StepResource {
			return fmt.Errorf("step %d must be a registered resource", i)
		}
	}
	return runApplyScheduleV2(
		ctx,
		steps,
		parallelism,
		func(ctx context.Context, step PlanStepV2) error {
			request, err := evaluate(ctx, step)
			if err != nil {
				return fmt.Errorf("evaluate resource: %w", err)
			}
			request.Step = step
			_, err = applyRegisteredResourceSnapshotStep(ctx, applyState, request)
			return err
		},
	)
}
