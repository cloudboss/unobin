package runtime

import (
	"context"
	"fmt"
)

func applyPlanFileV2(
	ctx context.Context,
	applyState *applyStateV2,
	plan PlanFileV2,
	callbacks applyPlanStepsV2Callbacks,
) error {
	if ctx == nil {
		return fmt.Errorf("apply context is required")
	}
	if applyState == nil {
		return fmt.Errorf("version 2 apply state is required")
	}
	if err := plan.Validate(); err != nil {
		return fmt.Errorf("saved plan: %w", err)
	}
	if err := validateApplyPlanStepsV2(plan.Steps, callbacks); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := applyStateMovesV2(ctx, applyState, plan.StateMoves); err != nil {
		return err
	}
	return applyPlanStepsV2(
		ctx,
		applyState,
		plan.Steps,
		plan.Parallelism,
		callbacks,
	)
}
