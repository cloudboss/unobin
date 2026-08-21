package runtime

import (
	"context"
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type applyPlanFileV2StatePreparer func(context.Context) (*applyStateV2, error)

func applyPlanFileV2WithStateLock(
	ctx context.Context,
	store state.Backend,
	factory state.FactoryInfo,
	plan PlanFileV2,
	prepareState applyPlanFileV2StatePreparer,
	callbacks applyPlanStepsV2Callbacks,
) (err error) {
	if ctx == nil {
		return fmt.Errorf("apply context is required")
	}
	if store == nil {
		return fmt.Errorf("state store is required")
	}
	if prepareState == nil {
		return fmt.Errorf("version 2 apply state preparer is required")
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

	release, err := AcquireStateLock(ctx, store)
	if err != nil {
		return err
	}
	defer func() {
		err = release(err)
	}()

	currentRevision, err := checkedCurrentRevision(store)
	if err != nil {
		return err
	}
	start := applyPlanFileV2Start{
		Factory:       factory,
		Stack:         store.Stack(),
		StateRevision: currentRevision,
	}
	if err := validateApplyPlanFileV2Start(start, plan); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	applyState, err := prepareState(ctx)
	if err != nil {
		return fmt.Errorf("prepare version 2 apply state: %w", err)
	}
	if applyState == nil {
		return fmt.Errorf("prepare version 2 apply state: callback returned nil state")
	}
	return applyPlanFileV2(ctx, applyState, start, plan, callbacks)
}
