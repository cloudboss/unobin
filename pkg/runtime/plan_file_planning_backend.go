package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type planFileV2StateRequest struct {
	Factory     state.FactoryInfo
	GeneratedAt time.Time
	Inputs      EncodedValue
	Backend     *StateRefV2
	Parallelism int
	Mode        PlanMode
	StateMoves  []PlannedEntryMove
	Evaluate    func(
		*state.SnapshotV2,
		*planningPassState,
	) ([]planStepV2Request, error)
}

func planFileV2PlanningSnapshots(
	store state.Backend,
) (planFileV2PlanningSnapshotCallbacks, error) {
	snapshots, ok := store.(state.SnapshotBackendV2)
	if !ok {
		return planFileV2PlanningSnapshotCallbacks{}, fmt.Errorf(
			"state store does not support version 2 snapshots",
		)
	}
	return planFileV2PlanningSnapshotCallbacks{Load: snapshots.GetV2}, nil
}

func planPlanFileV2FromState(
	ctx context.Context,
	store state.Backend,
	request planFileV2StateRequest,
) (PlanFileV2, error) {
	if ctx == nil {
		return PlanFileV2{}, fmt.Errorf("planning context is required")
	}
	if store == nil {
		return PlanFileV2{}, fmt.Errorf("state store is required")
	}
	snapshots, err := planFileV2PlanningSnapshots(store)
	if err != nil {
		return PlanFileV2{}, err
	}
	if request.Evaluate == nil {
		return PlanFileV2{}, fmt.Errorf("plan step evaluator is required")
	}
	if err := ctx.Err(); err != nil {
		return PlanFileV2{}, err
	}

	currentRevision, err := checkedCurrentRevision(store)
	if err != nil {
		return PlanFileV2{}, err
	}
	if err := ctx.Err(); err != nil {
		return PlanFileV2{}, err
	}
	stack := store.Stack()
	planRequest := planFileV2Request{
		Factory: FactoryRef{
			Name:            request.Factory.Name,
			Version:         request.Factory.Version,
			ContentRevision: request.Factory.ContentRevision,
		},
		Stack:         stack,
		StateRevision: currentRevision,
		GeneratedAt:   request.GeneratedAt,
		Inputs:        request.Inputs,
		Backend:       request.Backend,
		Parallelism:   request.Parallelism,
		Mode:          request.Mode,
		StateMoves:    request.StateMoves,
	}
	if _, err := preparePlanFileV2(planRequest); err != nil {
		return PlanFileV2{}, err
	}

	snapshot, err := preparePlanFileV2State(
		ctx,
		planFileV2PlanningStart{
			Factory:       request.Factory,
			Stack:         stack,
			StateRevision: currentRevision,
		},
		snapshots,
	)
	if err != nil {
		return PlanFileV2{}, fmt.Errorf("prepare version 2 planning state: %w", err)
	}
	if len(request.StateMoves) > 0 {
		if err := relocateSnapshotEntriesV2(snapshot, request.StateMoves); err != nil {
			return PlanFileV2{}, fmt.Errorf(
				"prepare version 2 planning state moves: %w",
				err,
			)
		}
	}

	planRequest.Evaluate = func(
		pass *planningPassState,
	) ([]planStepV2Request, error) {
		passSnapshot, err := snapshot.Clone()
		if err != nil {
			return nil, fmt.Errorf("copy version 2 planning snapshot: %w", err)
		}
		return request.Evaluate(passSnapshot, pass)
	}
	return planPlanFileV2(ctx, planRequest)
}
