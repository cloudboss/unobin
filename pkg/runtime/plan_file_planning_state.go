package runtime

import (
	"context"
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type planFileV2PlanningStart struct {
	Factory       state.FactoryInfo
	Stack         string
	StateRevision string
}

type planFileV2PlanningSnapshotCallbacks struct {
	Load func(string) (*state.SnapshotV2, error)
}

func preparePlanFileV2State(
	ctx context.Context,
	start planFileV2PlanningStart,
	snapshots planFileV2PlanningSnapshotCallbacks,
) (*state.SnapshotV2, error) {
	if ctx == nil {
		return nil, fmt.Errorf("planning context is required")
	}
	if snapshots.Load == nil {
		return nil, fmt.Errorf("version 2 snapshot loader is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var snapshot *state.SnapshotV2
	var err error
	if start.StateRevision == "" {
		snapshot, err = state.NewSnapshotV2(start.Factory, start.Stack)
		if err != nil {
			return nil, fmt.Errorf("initialize version 2 planning snapshot: %w", err)
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

	prepared, err := snapshot.Clone()
	if err != nil {
		return nil, fmt.Errorf("prepare version 2 planning snapshot: %w", err)
	}
	return prepared, nil
}
