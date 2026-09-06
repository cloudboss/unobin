package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestRelocateSnapshotEntriesV2MovesConfigurationReferences(t *testing.T) {
	snapshot := newPlanEvaluationV2Snapshot(t)
	composite := operationCompositeState(t, NodeResource)
	target := validOperationResourceTarget(t)
	target.Configuration.Address = "resource.old-app/library-config.cloud"
	target.DependsOn = []string{"resource.old-app"}
	configuration := cloneConfigurationRecord(target.Configuration)
	action := operationActionState(t)
	action.Configuration = configuration
	action.DependsOn = []string{
		"resource.app/resource.child", "resource.old-app/resource.child",
	}
	for _, entry := range []state.StateEntryV2{
		{Address: "resource.old-app", Kind: state.StateComposite,
			Payload: state.StatePayload{Kind: state.StateComposite, Composite: &composite}},
		{Address: "resource.old-app/resource.child", Kind: state.StateResource,
			Payload: state.StatePayload{
				Kind: state.StateResource, Resource: &state.ResourceStatePayload{Target: target},
			}},
		{Address: "action.consumer", Kind: state.StateAction,
			Payload: state.StatePayload{Kind: state.StateAction, Action: &action}},
	} {
		addPlanEvaluationV2Entry(t, snapshot, entry)
	}
	err := relocateSnapshotEntriesV2(snapshot, []PlannedEntryMove{
		{From: "resource.old-app", To: "resource.app"},
		{From: "resource.old-app/resource.child", To: "resource.app/resource.child"},
	})
	require.NoError(t, err)
	resource := snapshot.Find("resource.app/resource.child").Payload.Resource.Target
	require.Equal(t, "resource.app/library-config.cloud", resource.Configuration.Address)
	require.Equal(t, configuration.Digest, resource.Configuration.Digest)
	require.Equal(t, configuration.SensitiveValues, resource.Configuration.SensitiveValues)
	require.Equal(t, []string{"resource.app"}, resource.DependsOn)
	consumer := snapshot.Find("action.consumer").Payload.Action
	require.Equal(t, "resource.app/library-config.cloud", consumer.Configuration.Address)
	require.Equal(t, []string{"resource.app/resource.child"}, consumer.DependsOn)
}

func TestApplyStateMovesV2RelocatesEntriesAndDependencies(t *testing.T) {
	snapshot := newRegisteredApplySnapshot(t, nil)
	resourceTarget := validOperationResourceTarget(t)
	resourceTarget.DependsOn = []string{"data-source.old"}
	action := operationActionState(t)
	action.DependsOn = []string{"resource.old"}
	dataSource := operationDataSourceState(t)
	dataSource.DependsOn = []string{"action.old"}
	composite := operationCompositeState(t, NodeResource)
	composite.DependsOn = []string{"data-source.old"}

	for _, entry := range []state.StateEntryV2{
		{
			Address: "resource.old",
			Kind:    state.StateResource,
			Payload: state.StatePayload{
				Kind:     state.StateResource,
				Resource: &state.ResourceStatePayload{Target: resourceTarget},
			},
		},
		{
			Address: "action.old",
			Kind:    state.StateAction,
			Payload: state.StatePayload{
				Kind:   state.StateAction,
				Action: &action,
			},
		},
		{
			Address: "data-source.old",
			Kind:    state.StateDataSource,
			Payload: state.StatePayload{
				Kind:       state.StateDataSource,
				DataSource: &dataSource,
			},
		},
		{
			Address: "resource.old-app",
			Kind:    state.StateComposite,
			Payload: state.StatePayload{
				Kind:      state.StateComposite,
				Composite: &composite,
			},
		},
	} {
		require.NoError(t, snapshot.SetEntry(entry))
	}

	var persisted *state.SnapshotV2
	applyState, err := newApplyStateV2(
		snapshot,
		func(_ context.Context, next *state.SnapshotV2) error {
			persisted = next
			return nil
		},
	)
	require.NoError(t, err)
	generatedAt := time.Date(2026, time.August, 20, 16, 0, 0, 0, time.UTC)
	applyState.now = func() time.Time { return generatedAt }

	err = applyStateMovesV2(
		context.Background(),
		applyState,
		[]PlannedEntryMove{
			{From: "resource.old", To: "resource.api"},
			{From: "action.old", To: "action.notify"},
			{From: "data-source.old", To: "data-source.image"},
			{From: "resource.old-app", To: "resource.application"},
		},
	)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	require.Equal(t, generatedAt, persisted.GeneratedAt)
	require.Equal(t, []string{
		"action.notify",
		"data-source.image",
		"resource.api",
		"resource.application",
	}, stateMoveEntryAddresses(persisted))
	require.Equal(
		t,
		[]string{"resource.api"},
		persisted.Find("action.notify").Payload.Action.DependsOn,
	)
	require.Equal(
		t,
		[]string{"action.notify"},
		persisted.Find("data-source.image").Payload.DataSource.DependsOn,
	)
	require.Equal(
		t,
		[]string{"data-source.image"},
		persisted.Find("resource.application").Payload.Composite.DependsOn,
	)
	require.Equal(
		t,
		[]string{"data-source.image"},
		persisted.Find("resource.api").Payload.Resource.Target.DependsOn,
	)
	require.NotNil(t, snapshot.Find("resource.old"))
	require.Nil(t, snapshot.Find("resource.api"))

	current, err := applyState.snapshotCopy()
	require.NoError(t, err)
	require.Equal(t, persisted, current)
	persisted.Entries[0].Address = "action.changed"
	current, err = applyState.snapshotCopy()
	require.NoError(t, err)
	require.NotNil(t, current.Find("action.notify"))
}

func TestApplyStateMovesV2RelocatesOccupiedSourcesTogether(t *testing.T) {
	first := validOperationResourceTarget(t)
	first.DependsOn = []string{}
	second := first
	second.SchemaVersion = 2
	snapshot := newRegisteredApplySnapshot(t, map[string]ResourceTarget{
		"resource.a": first,
		"resource.b": second,
	})
	var persisted *state.SnapshotV2
	applyState, err := newApplyStateV2(
		snapshot,
		func(_ context.Context, next *state.SnapshotV2) error {
			persisted = next
			return nil
		},
	)
	require.NoError(t, err)

	err = applyStateMovesV2(
		context.Background(),
		applyState,
		[]PlannedEntryMove{
			{From: "resource.a", To: "resource.b"},
			{From: "resource.b", To: "resource.c"},
		},
	)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	require.Equal(t, 1, persisted.Find("resource.b").Payload.Resource.Target.SchemaVersion)
	require.Equal(t, 2, persisted.Find("resource.c").Payload.Resource.Target.SchemaVersion)
}

func TestApplyStateMovesV2RejectsInvalidMovesWithoutPersistence(t *testing.T) {
	tests := []struct {
		name    string
		moves   []PlannedEntryMove
		message string
	}{
		{
			name: "missing source",
			moves: []PlannedEntryMove{
				{From: "resource.missing", To: "resource.new"},
			},
			message: "no state entry at resource.missing",
		},
		{
			name: "occupied destination",
			moves: []PlannedEntryMove{
				{From: "resource.old", To: "resource.existing"},
			},
			message: "destination already exists at resource.existing",
		},
		{
			name: "invalid category",
			moves: []PlannedEntryMove{
				{From: "resource.old", To: "action.new"},
			},
			message: "address category action does not match resource",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := validOperationResourceTarget(t)
			target.DependsOn = []string{}
			snapshot := newRegisteredApplySnapshot(t, map[string]ResourceTarget{
				"resource.existing": target,
				"resource.old":      target,
			})
			persisted := false
			applyState, err := newApplyStateV2(
				snapshot,
				func(context.Context, *state.SnapshotV2) error {
					persisted = true
					return nil
				},
			)
			require.NoError(t, err)

			err = applyStateMovesV2(context.Background(), applyState, test.moves)
			require.ErrorContains(t, err, test.message)
			require.False(t, persisted)
			current, copyErr := applyState.snapshotCopy()
			require.NoError(t, copyErr)
			require.Equal(t, snapshot, current)
		})
	}
}

func TestApplyStateMovesV2KeepsStateOnPersistenceFailure(t *testing.T) {
	target := validOperationResourceTarget(t)
	target.DependsOn = []string{}
	snapshot := newRegisteredApplySnapshot(t, map[string]ResourceTarget{
		"resource.old": target,
	})
	expectedErr := errors.New("persistence failed")
	applyState, err := newApplyStateV2(
		snapshot,
		func(context.Context, *state.SnapshotV2) error { return expectedErr },
	)
	require.NoError(t, err)

	err = applyStateMovesV2(
		context.Background(),
		applyState,
		[]PlannedEntryMove{{From: "resource.old", To: "resource.new"}},
	)
	require.ErrorIs(t, err, expectedErr)
	current, copyErr := applyState.snapshotCopy()
	require.NoError(t, copyErr)
	require.Equal(t, snapshot, current)
}

func TestApplyStateMovesV2RejectsInvalidSetup(t *testing.T) {
	target := validOperationResourceTarget(t)
	target.DependsOn = []string{}
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, map[string]ResourceTarget{
			"resource.old": target,
		}),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)

	var missingContext context.Context
	err = applyStateMovesV2(
		missingContext,
		applyState,
		[]PlannedEntryMove{{From: "resource.old", To: "resource.new"}},
	)
	require.ErrorContains(t, err, "state-move context is required")

	err = applyStateMovesV2(
		context.Background(),
		nil,
		[]PlannedEntryMove{{From: "resource.old", To: "resource.new"}},
	)
	require.ErrorContains(t, err, "version 2 apply state is required")
}

func TestApplyStateMovesV2SkipsPersistenceWithoutMoves(t *testing.T) {
	persisted := false
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error {
			persisted = true
			return nil
		},
	)
	require.NoError(t, err)

	require.NoError(t, applyStateMovesV2(context.Background(), applyState, nil))
	require.False(t, persisted)
}

func stateMoveEntryAddresses(snapshot *state.SnapshotV2) []string {
	addresses := make([]string, len(snapshot.Entries))
	for i := range snapshot.Entries {
		addresses[i] = snapshot.Entries[i].Address
	}
	return addresses
}
