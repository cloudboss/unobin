package runtime

import (
	"context"
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type dataSourceSnapshotApplyRequest struct {
	Step    PlanStepV2
	Desired *PlannedDataSourceTarget
	Read    func(context.Context) (EncodedValue, error)
}

func (s *applyStateV2) dataSourceState(address string) (*DataSourceStatePayload, error) {
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
	if entry.Kind != state.StateDataSource || entry.Payload.DataSource == nil {
		return nil, fmt.Errorf("state entry %s is not a data source", address)
	}
	dataSource := *entry.Payload.DataSource
	return &dataSource, nil
}

func (s *applyStateV2) persistDataSourceState(
	ctx context.Context,
	address string,
	target *DataSourceStatePayload,
) error {
	if s == nil {
		return fmt.Errorf("apply state is required")
	}
	if ctx == nil {
		return fmt.Errorf("data-source persistence context is required")
	}
	return s.persistSnapshotUpdate(ctx, func(next *state.SnapshotV2) error {
		if target == nil {
			if err := next.RemoveEntry(address); err != nil {
				return fmt.Errorf("remove data-source state: %w", err)
			}
			return nil
		}
		if err := next.SetEntry(state.StateEntryV2{
			Address: address,
			Kind:    state.StateDataSource,
			Payload: state.StatePayload{
				Kind:       state.StateDataSource,
				DataSource: target,
			},
		}); err != nil {
			return fmt.Errorf("set data-source state: %w", err)
		}
		return nil
	})
}

func applyDataSourceSnapshotStep(
	ctx context.Context,
	applyState *applyStateV2,
	request dataSourceSnapshotApplyRequest,
) (*DataSourceStatePayload, error) {
	if ctx == nil {
		return nil, fmt.Errorf("data-source apply context is required")
	}
	if applyState == nil {
		return nil, fmt.Errorf("version 2 apply state is required")
	}
	if err := request.Step.Validate(); err != nil {
		return nil, fmt.Errorf("saved data-source step: %w", err)
	}
	if request.Step.Kind != NodeDataSource ||
		request.Step.Operation.Kind != StepDataSource {
		return nil, fmt.Errorf("saved data-source step must be a data source")
	}

	prior, err := applyState.dataSourceState(request.Step.Address)
	if err != nil {
		return nil, err
	}
	operation := request.Step.Operation.DataSource
	if operation.Prior != nil && prior == nil {
		return nil, fmt.Errorf(
			"saved plan requires prior data-source state at %s",
			request.Step.Address,
		)
	}
	if operation.Prior == nil && prior != nil {
		return nil, fmt.Errorf(
			"saved plan forbids prior data-source state at %s",
			request.Step.Address,
		)
	}

	return applyDataSourceOperation(
		ctx,
		dataSourceApplyRequest{
			Address:   request.Step.Address,
			Operation: *operation,
			Desired:   request.Desired,
			Prior:     prior,
			DependsOn: request.Step.DependsOn,
		},
		dataSourceApplyCallbacks{
			Read: request.Read,
			Persist: func(ctx context.Context, target *DataSourceStatePayload) error {
				return applyState.persistDataSourceState(
					ctx,
					request.Step.Address,
					target,
				)
			},
		},
	)
}
