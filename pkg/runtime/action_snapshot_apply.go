package runtime

import (
	"context"
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type actionSnapshotApplyRequest struct {
	Step    PlanStepV2
	Desired *PlannedActionTarget
	Run     func(context.Context) (EncodedValue, error)
}

func (s *applyStateV2) actionState(address string) (*ActionStatePayload, error) {
	if s == nil {
		return nil, fmt.Errorf("apply state is required")
	}
	snapshot, err := s.snapshotCopy()
	if err != nil {
		return nil, err
	}
	entry := snapshot.Find(address)
	if entry == nil {
		return nil, nil
	}
	if entry.Kind != state.StateAction || entry.Payload.Action == nil {
		return nil, fmt.Errorf("state entry %s is not an action", address)
	}
	action := *entry.Payload.Action
	return &action, nil
}

func (s *applyStateV2) persistActionState(
	ctx context.Context,
	address string,
	target *ActionStatePayload,
) error {
	if s == nil {
		return fmt.Errorf("apply state is required")
	}
	if ctx == nil {
		return fmt.Errorf("action persistence context is required")
	}
	return s.persistSnapshotUpdate(ctx, func(next *state.SnapshotV2) error {
		if target == nil {
			if err := next.RemoveEntry(address); err != nil {
				return fmt.Errorf("remove action state: %w", err)
			}
			return nil
		}
		if err := next.SetEntry(state.StateEntryV2{
			Address: address,
			Kind:    state.StateAction,
			Payload: state.StatePayload{
				Kind:   state.StateAction,
				Action: target,
			},
		}); err != nil {
			return fmt.Errorf("set action state: %w", err)
		}
		return nil
	})
}

func applyActionSnapshotStep(
	ctx context.Context,
	applyState *applyStateV2,
	request actionSnapshotApplyRequest,
) (*ActionStatePayload, error) {
	if ctx == nil {
		return nil, fmt.Errorf("action apply context is required")
	}
	if applyState == nil {
		return nil, fmt.Errorf("version 2 apply state is required")
	}
	if err := request.Step.Validate(); err != nil {
		return nil, fmt.Errorf("saved action step: %w", err)
	}
	if request.Step.Kind != NodeAction ||
		request.Step.Operation.Kind != StepAction {
		return nil, fmt.Errorf("saved action step must be an action")
	}

	prior, err := applyState.actionState(request.Step.Address)
	if err != nil {
		return nil, err
	}
	operation := request.Step.Operation.Action
	if operation.Prior != nil && prior == nil {
		return nil, fmt.Errorf(
			"saved plan requires prior action state at %s",
			request.Step.Address,
		)
	}
	if operation.Prior == nil && prior != nil {
		return nil, fmt.Errorf(
			"saved plan forbids prior action state at %s",
			request.Step.Address,
		)
	}

	return applyActionOperation(
		ctx,
		actionApplyRequest{
			Address:   request.Step.Address,
			Operation: *operation,
			Desired:   request.Desired,
			Prior:     prior,
			DependsOn: request.Step.DependsOn,
		},
		actionApplyCallbacks{
			Run: request.Run,
			Persist: func(ctx context.Context, target *ActionStatePayload) error {
				return applyState.persistActionState(
					ctx,
					request.Step.Address,
					target,
				)
			},
		},
	)
}
