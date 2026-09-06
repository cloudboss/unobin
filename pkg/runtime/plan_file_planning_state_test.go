package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestPreparePlanFileV2StateLoadsExactRevisionAndCopiesSnapshot(t *testing.T) {
	start := validPlanFileV2PlanningStart()
	loaded, err := state.NewSnapshotV2(start.Factory, start.Stack)
	require.NoError(t, err)
	require.NoError(t, loaded.SetOutputs(
		operationObject(t, map[string]EncodedValue{
			"secret": StringValue("value"),
		}),
		[]string{"/secret"},
	))
	loaded.Factory.ContentRevision = "prior-revision"
	loadedRevision := ""

	prepared, err := preparePlanFileV2State(
		context.Background(),
		start,
		planFileV2PlanningSnapshotCallbacks{
			Load: func(revision string) (*state.SnapshotV2, error) {
				loadedRevision = revision
				return loaded, nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, start.StateRevision, loadedRevision)
	require.Equal(t, loaded, prepared)
	require.NotSame(t, loaded, prepared)
	require.Equal(t, "prior-revision", prepared.Factory.ContentRevision)

	loaded.SensitivePaths[0] = "/changed"
	require.Equal(t, []string{"/secret"}, prepared.SensitivePaths)
}

func TestPreparePlanFileV2StateInitializesNewSnapshot(t *testing.T) {
	start := validPlanFileV2PlanningStart()
	start.StateRevision = ""
	loaded := false

	prepared, err := preparePlanFileV2State(
		context.Background(),
		start,
		planFileV2PlanningSnapshotCallbacks{
			Load: func(string) (*state.SnapshotV2, error) {
				loaded = true
				return nil, errors.New("unexpected snapshot load")
			},
		},
	)
	require.NoError(t, err)
	require.False(t, loaded)
	require.NoError(t, prepared.Validate())
	require.Equal(t, state.CurrentFormatVersion, prepared.FormatVersion)
	require.Equal(t, start.Factory, prepared.Factory)
	require.Equal(t, start.Stack, prepared.Stack)
	require.NotNil(t, prepared.Entries)
	require.Empty(t, prepared.Entries)
	outputs, object := prepared.Outputs.ObjectFields()
	require.True(t, object)
	require.Empty(t, outputs)
	require.NotNil(t, prepared.SensitivePaths)
	require.Empty(t, prepared.SensitivePaths)
}

func TestPreparePlanFileV2StateRejectsInvalidSetup(t *testing.T) {
	start := validPlanFileV2PlanningStart()
	called := false
	callbacks := planFileV2PlanningSnapshotCallbacks{
		Load: func(string) (*state.SnapshotV2, error) {
			called = true
			return nil, nil
		},
	}

	var missingContext context.Context
	prepared, err := preparePlanFileV2State(missingContext, start, callbacks)
	require.ErrorContains(t, err, "planning context is required")
	require.Nil(t, prepared)
	require.False(t, called)

	prepared, err = preparePlanFileV2State(
		context.Background(),
		start,
		planFileV2PlanningSnapshotCallbacks{},
	)
	require.ErrorContains(t, err, "version 2 snapshot loader is required")
	require.Nil(t, prepared)
	require.False(t, called)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prepared, err = preparePlanFileV2State(ctx, start, callbacks)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, prepared)
	require.False(t, called)
}

func TestPreparePlanFileV2StateRejectsInvalidNewSnapshotMetadata(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*planFileV2PlanningStart)
		message string
	}{
		{
			name: "factory",
			change: func(start *planFileV2PlanningStart) {
				start.Factory.ContentRevision = ""
			},
			message: "factory content revision is required",
		},
		{
			name:    "stack",
			change:  func(start *planFileV2PlanningStart) { start.Stack = "" },
			message: "stack is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start := validPlanFileV2PlanningStart()
			start.StateRevision = ""
			test.change(&start)
			called := false

			prepared, err := preparePlanFileV2State(
				context.Background(),
				start,
				planFileV2PlanningSnapshotCallbacks{
					Load: func(string) (*state.SnapshotV2, error) {
						called = true
						return nil, nil
					},
				},
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, prepared)
			require.False(t, called)
		})
	}
}

func TestPreparePlanFileV2StateRejectsSnapshotLoadFailures(t *testing.T) {
	start := validPlanFileV2PlanningStart()
	expectedErr := errors.New("snapshot unavailable")
	invalid, err := state.NewSnapshotV2(start.Factory, start.Stack)
	require.NoError(t, err)
	invalid.Entries = nil

	tests := []struct {
		name    string
		load    func(string) (*state.SnapshotV2, error)
		message string
		cause   error
	}{
		{
			name: "load error",
			load: func(string) (*state.SnapshotV2, error) {
				return nil, expectedErr
			},
			message: "load version 2 snapshot",
			cause:   expectedErr,
		},
		{
			name:    "nil snapshot",
			load:    func(string) (*state.SnapshotV2, error) { return nil, nil },
			message: "loader returned nil snapshot",
		},
		{
			name: "invalid snapshot",
			load: func(string) (*state.SnapshotV2, error) {
				return invalid, nil
			},
			message: "entries are required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := preparePlanFileV2State(
				context.Background(),
				start,
				planFileV2PlanningSnapshotCallbacks{Load: test.load},
			)
			require.ErrorContains(t, err, test.message)
			if test.cause != nil {
				require.ErrorIs(t, err, test.cause)
			}
			require.Nil(t, prepared)
		})
	}
}

func TestPreparePlanFileV2StateStopsAfterLoadCancellation(t *testing.T) {
	start := validPlanFileV2PlanningStart()
	loaded, err := state.NewSnapshotV2(start.Factory, start.Stack)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())

	prepared, err := preparePlanFileV2State(
		ctx,
		start,
		planFileV2PlanningSnapshotCallbacks{
			Load: func(string) (*state.SnapshotV2, error) {
				cancel()
				return loaded, nil
			},
		},
	)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, prepared)
}

func validPlanFileV2PlanningStart() planFileV2PlanningStart {
	return planFileV2PlanningStart{
		Factory: state.FactoryInfo{
			Name:            "deploy",
			Version:         "v1.0.0",
			ContentRevision: "current-revision",
		},
		Stack:         "production",
		StateRevision: "state-1",
	}
}
