package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type applyStateV2 struct {
	mu              sync.Mutex
	snapshot        *state.SnapshotV2
	persistSnapshot func(context.Context, *state.SnapshotV2) error
	now             func() time.Time
}

type registeredResourceSnapshotApplyRequest struct {
	Step                PlanStepV2
	Desired             *PlannedResourceTarget
	DesiredConfigType   *resolvedConfigurationDefinition
	DesiredRegistration *resourceDefinitionRegistration
	PriorConfigType     *resolvedConfigurationDefinition
	PriorRegistration   *resourceDefinitionRegistration
}

func newApplyStateV2(
	snapshot *state.SnapshotV2,
	persist func(context.Context, *state.SnapshotV2) error,
) (*applyStateV2, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot is required")
	}
	if persist == nil {
		return nil, fmt.Errorf("snapshot persistence callback is required")
	}
	cloned, err := snapshot.Clone()
	if err != nil {
		return nil, fmt.Errorf("prepare apply snapshot: %w", err)
	}
	return &applyStateV2{
		snapshot:        cloned,
		persistSnapshot: persist,
		now:             time.Now,
	}, nil
}

func (s *applyStateV2) snapshotCopy() (*state.SnapshotV2, error) {
	if s == nil {
		return nil, fmt.Errorf("apply state is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned, err := s.snapshot.Clone()
	if err != nil {
		return nil, fmt.Errorf("copy apply snapshot: %w", err)
	}
	return cloned, nil
}

func (s *applyStateV2) resourceTarget(address string) (*ResourceTarget, error) {
	if s == nil {
		return nil, fmt.Errorf("apply state is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned, err := s.snapshot.Clone()
	if err != nil {
		return nil, fmt.Errorf("copy apply snapshot: %w", err)
	}
	entry := cloned.Find(address)
	if entry == nil {
		return nil, nil
	}
	if entry.Kind != state.StateResource || entry.Payload.Resource == nil {
		return nil, fmt.Errorf("state entry %s is not a resource", address)
	}
	target := entry.Payload.Resource.Target
	return &target, nil
}

func (s *applyStateV2) persistResourceTarget(
	ctx context.Context,
	address string,
	target *ResourceTarget,
) error {
	if s == nil {
		return fmt.Errorf("apply state is required")
	}
	if ctx == nil {
		return fmt.Errorf("resource persistence context is required")
	}
	return s.persistSnapshotUpdate(ctx, func(next *state.SnapshotV2) error {
		if target == nil {
			if err := next.RemoveEntry(address); err != nil {
				return fmt.Errorf("remove resource state: %w", err)
			}
			return nil
		}
		if err := next.SetEntry(state.StateEntryV2{
			Address: address,
			Kind:    state.StateResource,
			Payload: state.StatePayload{
				Kind: state.StateResource,
				Resource: &state.ResourceStatePayload{
					Target: *target,
				},
			},
		}); err != nil {
			return fmt.Errorf("set resource state: %w", err)
		}
		return nil
	})
}

func (s *applyStateV2) persistOutputs(
	ctx context.Context,
	outputs EncodedValue,
	sensitivePaths []string,
) error {
	if s == nil {
		return fmt.Errorf("apply state is required")
	}
	if ctx == nil {
		return fmt.Errorf("output persistence context is required")
	}
	return s.persistSnapshotUpdate(ctx, func(next *state.SnapshotV2) error {
		if err := next.SetOutputs(outputs, sensitivePaths); err != nil {
			return fmt.Errorf("set snapshot outputs: %w", err)
		}
		return nil
	})
}

func (s *applyStateV2) persistSnapshotUpdate(
	ctx context.Context,
	update func(*state.SnapshotV2) error,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := s.snapshot.Clone()
	if err != nil {
		return fmt.Errorf("copy apply snapshot: %w", err)
	}
	if err := update(next); err != nil {
		return err
	}
	next.GeneratedAt = s.now().UTC()
	toPersist, err := next.Clone()
	if err != nil {
		return fmt.Errorf("copy snapshot for persistence: %w", err)
	}
	if err := s.persistSnapshot(ctx, toPersist); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	s.snapshot = next
	return nil
}

func applyRegisteredResourceSnapshotStep(
	ctx context.Context,
	applyState *applyStateV2,
	request registeredResourceSnapshotApplyRequest,
) (*ResourceTarget, error) {
	if ctx == nil {
		return nil, fmt.Errorf("resource apply context is required")
	}
	if applyState == nil {
		return nil, fmt.Errorf("version 2 apply state is required")
	}
	if err := request.Step.Validate(); err != nil {
		return nil, fmt.Errorf("saved resource step: %w", err)
	}
	if request.Step.Kind != NodeResource {
		return nil, fmt.Errorf("saved resource step must be a resource")
	}

	prior, err := applyState.resourceTarget(request.Step.Address)
	if err != nil {
		return nil, err
	}
	operation := request.Step.Operation.Resource
	if operation.Prior != nil && prior == nil {
		return nil, fmt.Errorf(
			"saved plan requires prior resource state at %s",
			request.Step.Address,
		)
	}
	if operation.Prior == nil && prior != nil {
		return nil, fmt.Errorf(
			"saved plan forbids prior resource state at %s",
			request.Step.Address,
		)
	}

	return applyRegisteredResourceOperation(
		ctx,
		registeredResourceApplyOperationRequest{
			Step:                request.Step,
			Desired:             request.Desired,
			DesiredConfigType:   request.DesiredConfigType,
			DesiredRegistration: request.DesiredRegistration,
			Prior:               prior,
			PriorConfigType:     request.PriorConfigType,
			PriorRegistration:   request.PriorRegistration,
			Persist: func(ctx context.Context, target *ResourceTarget) error {
				return applyState.persistResourceTarget(
					ctx,
					request.Step.Address,
					target,
				)
			},
		},
	)
}
