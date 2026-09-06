package runtime

import (
	"context"
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type applyPlanFileV2Start struct {
	Factory       state.FactoryInfo
	Stack         string
	StateRevision string
}

func applyPlanFileV2(
	ctx context.Context,
	applyState *applyStateV2,
	start applyPlanFileV2Start,
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
	if err := validateApplyPlanFileV2Start(start, plan); err != nil {
		return err
	}
	if err := validateApplyPlanStepsV2(plan.Steps, callbacks); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if callbacks.Prepare != nil {
		if err := callbacks.Prepare(ctx, applyState); err != nil {
			return fmt.Errorf("prepare factory apply: %w", err)
		}
	}
	if err := applyState.prepareSnapshot(start.Factory, start.Stack); err != nil {
		return fmt.Errorf("prepare version 2 apply snapshot: %w", err)
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

func validateApplyPlanFileV2Start(
	start applyPlanFileV2Start,
	plan PlanFileV2,
) error {
	if start.Factory.Name != plan.Factory.Name ||
		start.Factory.Version != plan.Factory.Version ||
		start.Factory.ContentRevision != plan.Factory.ContentRevision {
		return fmt.Errorf(
			"saved plan factory does not match the running factory: "+
				"planned %s %s (%s), running %s %s (%s)",
			plan.Factory.Name,
			plan.Factory.Version,
			plan.Factory.ContentRevision,
			start.Factory.Name,
			start.Factory.Version,
			start.Factory.ContentRevision,
		)
	}
	if start.Stack != plan.Stack {
		return fmt.Errorf(
			"saved plan stack does not match the apply stack: planned %q, apply %q",
			plan.Stack,
			start.Stack,
		)
	}
	if start.StateRevision != plan.StateRevision {
		return fmt.Errorf(
			"state revision changed from %q to %q; create a new plan",
			plan.StateRevision,
			start.StateRevision,
		)
	}
	return nil
}
