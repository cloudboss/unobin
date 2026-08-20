package runtime

import (
	"context"
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type compositeSnapshotApplyRequest struct {
	Step    PlanStepV2
	Desired *PlannedCompositeTarget
	Eval    func(context.Context) (EncodedValue, error)
}

func (s *applyStateV2) compositeState(address string) (*CompositeStatePayload, error) {
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
	if entry.Kind != state.StateComposite || entry.Payload.Composite == nil {
		return nil, fmt.Errorf("state entry %s is not a composite", address)
	}
	composite := *entry.Payload.Composite
	return &composite, nil
}

func (s *applyStateV2) persistCompositeState(
	ctx context.Context,
	address string,
	target *CompositeStatePayload,
) error {
	if s == nil {
		return fmt.Errorf("apply state is required")
	}
	if ctx == nil {
		return fmt.Errorf("composite persistence context is required")
	}
	return s.persistSnapshotUpdate(ctx, func(next *state.SnapshotV2) error {
		if target == nil {
			if err := next.RemoveEntry(address); err != nil {
				return fmt.Errorf("remove composite state: %w", err)
			}
			return nil
		}
		if err := next.SetEntry(state.StateEntryV2{
			Address: address,
			Kind:    state.StateComposite,
			Payload: state.StatePayload{
				Kind:      state.StateComposite,
				Composite: target,
			},
		}); err != nil {
			return fmt.Errorf("set composite state: %w", err)
		}
		return nil
	})
}

func applyCompositeSnapshotStep(
	ctx context.Context,
	applyState *applyStateV2,
	request compositeSnapshotApplyRequest,
) (*CompositeStatePayload, error) {
	if ctx == nil {
		return nil, fmt.Errorf("composite apply context is required")
	}
	if applyState == nil {
		return nil, fmt.Errorf("version 2 apply state is required")
	}
	if err := request.Step.Validate(); err != nil {
		return nil, fmt.Errorf("saved composite step: %w", err)
	}
	if request.Step.Operation.Kind != StepComposite {
		return nil, fmt.Errorf("saved composite step must be a composite")
	}

	prior, err := applyState.compositeState(request.Step.Address)
	if err != nil {
		return nil, err
	}
	operation := request.Step.Operation.Composite
	if operation.Prior != nil && prior == nil {
		return nil, fmt.Errorf(
			"saved plan requires prior composite state at %s",
			request.Step.Address,
		)
	}
	if operation.Prior == nil && prior != nil {
		return nil, fmt.Errorf(
			"saved plan forbids prior composite state at %s",
			request.Step.Address,
		)
	}

	return applyCompositeOperation(
		ctx,
		compositeApplyRequest{
			Address:   request.Step.Address,
			Category:  request.Step.Kind,
			Operation: *operation,
			Desired:   request.Desired,
			Prior:     prior,
			DependsOn: request.Step.DependsOn,
		},
		compositeApplyCallbacks{
			Eval: request.Eval,
			Persist: func(ctx context.Context, target *CompositeStatePayload) error {
				return applyState.persistCompositeState(
					ctx,
					request.Step.Address,
					target,
				)
			},
		},
	)
}
