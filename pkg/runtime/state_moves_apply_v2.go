package runtime

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func applyStateMovesV2(
	ctx context.Context,
	applyState *applyStateV2,
	moves []PlannedEntryMove,
) error {
	if ctx == nil {
		return fmt.Errorf("state-move context is required")
	}
	if applyState == nil {
		return fmt.Errorf("version 2 apply state is required")
	}
	if err := validatePlanMoves(moves); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(moves) == 0 {
		return nil
	}

	return applyState.persistSnapshotUpdate(ctx, func(next *state.SnapshotV2) error {
		if err := relocateSnapshotEntriesV2(next, moves); err != nil {
			return fmt.Errorf("apply state moves: %w", err)
		}
		return nil
	})
}

func relocateSnapshotEntriesV2(
	snapshot *state.SnapshotV2,
	moves []PlannedEntryMove,
) error {
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	bySource := make(map[string]string, len(moves))
	for _, move := range moves {
		if snapshot.Find(move.From) == nil {
			return fmt.Errorf("no state entry at %s", move.From)
		}
		bySource[move.From] = move.To
	}
	for _, move := range moves {
		_, destinationMoves := bySource[move.To]
		if snapshot.Find(move.To) != nil && !destinationMoves {
			return fmt.Errorf("destination already exists at %s", move.To)
		}
	}

	for i := range snapshot.Entries {
		entry := &snapshot.Entries[i]
		if destination, ok := bySource[entry.Address]; ok {
			entry.Address = destination
		}
		rewriteStateEntryV2Dependencies(entry, bySource)
	}
	slices.SortFunc(snapshot.Entries, func(a, b state.StateEntryV2) int {
		return strings.Compare(a.Address, b.Address)
	})
	return snapshot.Validate()
}

func rewriteStateEntryV2Dependencies(
	entry *state.StateEntryV2,
	moves map[string]string,
) {
	dependencies := stateEntryV2Dependencies(entry)
	for i := range dependencies {
		if destination, ok := moves[dependencies[i]]; ok {
			dependencies[i] = destination
		}
	}
	slices.Sort(dependencies)
}

func stateEntryV2Dependencies(entry *state.StateEntryV2) []string {
	switch entry.Kind {
	case state.StateResource:
		return entry.Payload.Resource.Target.DependsOn
	case state.StateAction:
		return entry.Payload.Action.DependsOn
	case state.StateDataSource:
		return entry.Payload.DataSource.DependsOn
	case state.StateComposite:
		return entry.Payload.Composite.DependsOn
	default:
		return nil
	}
}
