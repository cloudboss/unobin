package runner

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func readStateSnapshot(store state.Backend, revision string) (*state.SnapshotV2, error) {
	snapshots, ok := store.(state.SnapshotBackendV2)
	if !ok {
		return nil, fmt.Errorf("state backend does not support version 2 snapshots")
	}
	if revision == "" {
		current, err := store.CurrentRev()
		if err != nil {
			return nil, err
		}
		revision = current
	}
	return snapshots.GetV2(revision)
}

func writeStateSnapshot(store state.Backend, snapshot *state.SnapshotV2) (string, error) {
	snapshots, ok := store.(state.SnapshotBackendV2)
	if !ok {
		return "", fmt.Errorf("state backend does not support version 2 snapshots")
	}
	return snapshots.WriteV2(snapshot)
}
