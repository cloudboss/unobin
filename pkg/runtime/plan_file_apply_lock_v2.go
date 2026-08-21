package runtime

import (
	"context"
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type applyPlanFileV2SnapshotCallbacks struct {
	Load       func(string) (*state.SnapshotV2, error)
	Write      func(*state.SnapshotV2) (string, error)
	SetCurrent func(string) error
}

func (c applyPlanFileV2SnapshotCallbacks) persist(
	ctx context.Context,
	snapshot *state.SnapshotV2,
) error {
	if ctx == nil {
		return fmt.Errorf("snapshot persistence context is required")
	}
	if snapshot == nil {
		return fmt.Errorf("version 2 snapshot is required")
	}
	if c.Write == nil {
		return fmt.Errorf("version 2 snapshot writer is required")
	}
	if c.SetCurrent == nil {
		return fmt.Errorf("version 2 current snapshot setter is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	revision, err := c.Write(snapshot)
	if err != nil {
		return fmt.Errorf("write version 2 snapshot: %w", err)
	}
	if err := c.SetCurrent(revision); err != nil {
		return fmt.Errorf("set current version 2 snapshot %q: %w", revision, err)
	}
	return nil
}

func applyPlanFileV2WithStateLock(
	ctx context.Context,
	store state.Backend,
	factory state.FactoryInfo,
	plan PlanFileV2,
	snapshots applyPlanFileV2SnapshotCallbacks,
	callbacks applyPlanStepsV2Callbacks,
) (err error) {
	if ctx == nil {
		return fmt.Errorf("apply context is required")
	}
	if store == nil {
		return fmt.Errorf("state store is required")
	}
	if snapshots.Load == nil {
		return fmt.Errorf("version 2 snapshot loader is required")
	}
	if snapshots.Write == nil {
		return fmt.Errorf("version 2 snapshot writer is required")
	}
	if snapshots.SetCurrent == nil {
		return fmt.Errorf("version 2 current snapshot setter is required")
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

	applyState, err := prepareApplyPlanFileV2State(ctx, start, snapshots)
	if err != nil {
		return fmt.Errorf("prepare version 2 apply state: %w", err)
	}
	return applyPlanFileV2(ctx, applyState, start, plan, callbacks)
}

func prepareApplyPlanFileV2State(
	ctx context.Context,
	start applyPlanFileV2Start,
	snapshots applyPlanFileV2SnapshotCallbacks,
) (*applyStateV2, error) {
	if ctx == nil {
		return nil, fmt.Errorf("apply context is required")
	}
	if snapshots.Load == nil {
		return nil, fmt.Errorf("version 2 snapshot loader is required")
	}
	if snapshots.Write == nil {
		return nil, fmt.Errorf("version 2 snapshot writer is required")
	}
	if snapshots.SetCurrent == nil {
		return nil, fmt.Errorf("version 2 current snapshot setter is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var snapshot *state.SnapshotV2
	var err error
	if start.StateRevision == "" {
		snapshot, err = state.NewSnapshotV2(start.Factory, start.Stack)
		if err != nil {
			return nil, fmt.Errorf("initialize version 2 snapshot: %w", err)
		}
	} else {
		snapshot, err = snapshots.Load(start.StateRevision)
		if err != nil {
			return nil, fmt.Errorf(
				"load version 2 snapshot %q: %w",
				start.StateRevision,
				err,
			)
		}
		if snapshot == nil {
			return nil, fmt.Errorf(
				"load version 2 snapshot %q: loader returned nil snapshot",
				start.StateRevision,
			)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	applyState, err := newApplyStateV2(snapshot, snapshots.persist)
	if err != nil {
		return nil, fmt.Errorf("initialize version 2 apply state: %w", err)
	}
	return applyState, nil
}
