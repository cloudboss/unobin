package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func newRegisteredApplySnapshot(
	t *testing.T,
	targets map[string]ResourceTarget,
) *state.SnapshotV2 {
	t.Helper()
	snapshot, err := state.NewSnapshotV2(
		state.FactoryInfo{
			Name:            "factory",
			Version:         "1.0.0",
			ContentRevision: strings.Repeat("f", 64),
		},
		"production",
	)
	require.NoError(t, err)
	snapshot.GeneratedAt = time.Unix(1, 0).UTC()
	for address, target := range targets {
		require.NoError(t, snapshot.SetEntry(state.StateEntryV2{
			Address: address,
			Kind:    state.StateResource,
			Payload: state.StatePayload{
				Kind: state.StateResource,
				Resource: &state.ResourceStatePayload{
					Target: target,
				},
			},
		}))
	}
	return snapshot
}

func TestApplyRegisteredResourceSnapshotStepCreatesState(t *testing.T) {
	capture := &registeredApplyCapture{
		createResult: &registeredApplyOutput{ID: "object-1", Value: "created"},
	}
	definition := registeredApplyDefinition(1, nil)
	registration := newRegisteredApplyResource(t, definition, capture)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
	desired := registeredApplyDesired(
		t,
		registration,
		binding,
		configuration,
		registeredApplyInput{Name: "logs", Size: 1},
	)
	operation, err := registration.planResourceOperation(resourcePlanningRequest{
		Desired: &desired,
	})
	require.NoError(t, err)

	original := newRegisteredApplySnapshot(t, nil)
	var persisted *state.SnapshotV2
	applyState, err := newApplyStateV2(
		original,
		func(_ context.Context, snapshot *state.SnapshotV2) error {
			capture.calls = append(capture.calls, "persist")
			persisted = snapshot
			return nil
		},
	)
	require.NoError(t, err)
	generatedAt := time.Date(2026, time.August, 19, 16, 0, 0, 0, time.UTC)
	applyState.now = func() time.Time { return generatedAt }

	target, err := applyRegisteredResourceSnapshotStep(
		context.Background(),
		applyState,
		registeredResourceSnapshotApplyRequest{
			Step: registeredApplyStep(
				t,
				"resource.logs",
				[]string{"resource.network"},
				operation,
			),
			Desired:             &desired,
			DesiredConfigType:   configurationDefinition,
			DesiredRegistration: registration,
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"create:current", "persist"}, capture.calls)
	require.Empty(t, original.Entries)
	require.NotNil(t, persisted)
	require.Equal(t, generatedAt, persisted.GeneratedAt)

	current, err := applyState.snapshotCopy()
	require.NoError(t, err)
	require.Equal(t, persisted, current)
	entry := current.Find("resource.logs")
	require.NotNil(t, entry)
	require.Equal(t, target, &entry.Payload.Resource.Target)
	require.Equal(t, []string{"resource.network"}, target.DependsOn)

	persisted.Entries[0].Payload.Resource.Target.DependsOn[0] = "resource.changed"
	current, err = applyState.snapshotCopy()
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"resource.network"},
		current.Find("resource.logs").Payload.Resource.Target.DependsOn,
	)
}

func TestApplyRegisteredResourceSnapshotStepRemovesDestroyedState(t *testing.T) {
	capture := &registeredApplyCapture{
		readResult: &registeredApplyOutput{ID: "object-1", Value: "fresh"},
	}
	definition := registeredApplyDefinition(1, nil)
	registration := newRegisteredApplyResource(t, definition, capture)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"prior",
	)
	prior := registeredApplyTarget(
		t,
		definition,
		registration,
		binding,
		configuration,
		1,
		registeredApplyInput{Name: "logs", Size: 1},
		&registeredApplyOutput{ID: "object-1", Value: "recorded"},
	)
	operation, err := runFixedPointPlanning(
		context.Background(),
		func(pass *planningPassState) (*ResourcePlanOperation, error) {
			return planRegisteredResourceOperation(
				context.Background(),
				pass,
				registeredResourcePlanningRequest{
					Address:           "resource.logs",
					Prior:             &prior,
					PriorConfigType:   configurationDefinition,
					PriorRegistration: registration,
				},
			)
		},
	)
	require.NoError(t, err)
	require.Equal(t, DecisionDestroy, operation.Decision)
	capture.calls = nil

	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, map[string]ResourceTarget{
			"resource.logs": prior,
		}),
		func(_ context.Context, _ *state.SnapshotV2) error {
			capture.calls = append(capture.calls, "persist")
			return nil
		},
	)
	require.NoError(t, err)

	target, err := applyRegisteredResourceSnapshotStep(
		context.Background(),
		applyState,
		registeredResourceSnapshotApplyRequest{
			Step:              registeredApplyStep(t, "resource.logs", []string{}, operation),
			PriorConfigType:   configurationDefinition,
			PriorRegistration: registration,
		},
	)
	require.NoError(t, err)
	require.Nil(t, target)
	require.Equal(
		t,
		[]string{"read:prior", "delete:prior", "persist"},
		capture.calls,
	)
	current, err := applyState.snapshotCopy()
	require.NoError(t, err)
	require.Nil(t, current.Find("resource.logs"))
}

func TestApplyRegisteredResourceSnapshotStepRequiresSavedPriorState(t *testing.T) {
	capture := &registeredApplyCapture{
		createResult: &registeredApplyOutput{ID: "object-1", Value: "updated"},
		readResult:   &registeredApplyOutput{ID: "object-1", Value: "observed"},
	}
	definition := registeredApplyDefinition(1, nil)
	registration := newRegisteredApplyResource(t, definition, capture)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
	prior := registeredApplyTarget(
		t,
		definition,
		registration,
		binding,
		configuration,
		1,
		registeredApplyInput{Name: "logs", Size: 1},
		&registeredApplyOutput{ID: "object-1", Value: "recorded"},
	)
	desired := registeredApplyDesired(
		t,
		registration,
		binding,
		configuration,
		registeredApplyInput{Name: "logs", Size: 2},
	)
	operation, err := runFixedPointPlanning(
		context.Background(),
		func(pass *planningPassState) (*ResourcePlanOperation, error) {
			return planRegisteredResourceOperation(
				context.Background(),
				pass,
				registeredResourcePlanningRequest{
					Address:             "resource.logs",
					Desired:             &desired,
					DesiredConfigType:   configurationDefinition,
					DesiredRegistration: registration,
					Prior:               &prior,
					PriorConfigType:     configurationDefinition,
					PriorRegistration:   registration,
				},
			)
		},
	)
	require.NoError(t, err)
	changedPrior := registeredApplyTarget(
		t,
		definition,
		registration,
		binding,
		configuration,
		1,
		registeredApplyInput{Name: "logs", Size: 3},
		&registeredApplyOutput{ID: "object-1", Value: "recorded"},
	)
	tests := []struct {
		name    string
		targets map[string]ResourceTarget
		message string
	}{
		{
			name:    "missing",
			message: "saved plan requires prior resource state",
		},
		{
			name: "changed",
			targets: map[string]ResourceTarget{
				"resource.logs": changedPrior,
			},
			message: "prior target does not match the saved plan",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capture.calls = nil
			persisted := false
			applyState, err := newApplyStateV2(
				newRegisteredApplySnapshot(t, test.targets),
				func(_ context.Context, _ *state.SnapshotV2) error {
					persisted = true
					return nil
				},
			)
			require.NoError(t, err)

			target, err := applyRegisteredResourceSnapshotStep(
				context.Background(),
				applyState,
				registeredResourceSnapshotApplyRequest{
					Step: registeredApplyStep(
						t,
						"resource.logs",
						[]string{},
						operation,
					),
					Desired:             &desired,
					DesiredConfigType:   configurationDefinition,
					DesiredRegistration: registration,
					PriorConfigType:     configurationDefinition,
					PriorRegistration:   registration,
				},
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
			require.Empty(t, capture.calls)
			require.False(t, persisted)
		})
	}
}

func TestApplyRegisteredResourceSnapshotStepKeepsStateOnPersistenceFailure(t *testing.T) {
	capture := &registeredApplyCapture{
		createResult: &registeredApplyOutput{ID: "object-1", Value: "created"},
	}
	definition := registeredApplyDefinition(1, nil)
	registration := newRegisteredApplyResource(t, definition, capture)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
	desired := registeredApplyDesired(
		t,
		registration,
		binding,
		configuration,
		registeredApplyInput{Name: "logs", Size: 1},
	)
	operation, err := registration.planResourceOperation(resourcePlanningRequest{
		Desired: &desired,
	})
	require.NoError(t, err)

	expectedErr := errors.New("state write failed")
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(_ context.Context, _ *state.SnapshotV2) error {
			return expectedErr
		},
	)
	require.NoError(t, err)
	before, err := applyState.snapshotCopy()
	require.NoError(t, err)

	target, err := applyRegisteredResourceSnapshotStep(
		context.Background(),
		applyState,
		registeredResourceSnapshotApplyRequest{
			Step:                registeredApplyStep(t, "resource.logs", []string{}, operation),
			Desired:             &desired,
			DesiredConfigType:   configurationDefinition,
			DesiredRegistration: registration,
		},
	)
	require.ErrorIs(t, err, expectedErr)
	require.Nil(t, target)
	require.Equal(t, []string{"create:current"}, capture.calls)
	after, copyErr := applyState.snapshotCopy()
	require.NoError(t, copyErr)
	require.Equal(t, before, after)
}

func TestApplyStateV2SerializesResourcePersistence(t *testing.T) {
	capture := &registeredApplyCapture{}
	definition := registeredApplyDefinition(1, nil)
	registration := newRegisteredApplyResource(t, definition, capture)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	_, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
	targets := map[string]ResourceTarget{
		"resource.api": registeredApplyTarget(
			t,
			definition,
			registration,
			binding,
			configuration,
			1,
			registeredApplyInput{Name: "api", Size: 1},
			&registeredApplyOutput{ID: "api-1"},
		),
		"resource.worker": registeredApplyTarget(
			t,
			definition,
			registration,
			binding,
			configuration,
			1,
			registeredApplyInput{Name: "worker", Size: 1},
			&registeredApplyOutput{ID: "worker-1"},
		),
	}
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(_ context.Context, _ *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)

	start := make(chan struct{})
	errorsByAddress := make(map[string]error, len(targets))
	var mu sync.Mutex
	var wait sync.WaitGroup
	for address, target := range targets {
		wait.Go(func() {
			<-start
			err := applyState.persistResourceTarget(context.Background(), address, &target)
			mu.Lock()
			errorsByAddress[address] = err
			mu.Unlock()
		})
	}
	close(start)
	wait.Wait()
	require.Equal(t, map[string]error{
		"resource.api":    nil,
		"resource.worker": nil,
	}, errorsByAddress)

	current, err := applyState.snapshotCopy()
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"resource.api", "resource.worker"},
		[]string{current.Entries[0].Address, current.Entries[1].Address},
	)
}

func TestNewApplyStateV2RejectsInvalidSetup(t *testing.T) {
	valid := newRegisteredApplySnapshot(t, nil)
	tests := []struct {
		name     string
		snapshot *state.SnapshotV2
		persist  func(context.Context, *state.SnapshotV2) error
		message  string
	}{
		{
			name:    "missing snapshot",
			persist: func(context.Context, *state.SnapshotV2) error { return nil },
			message: "snapshot is required",
		},
		{
			name:     "missing persistence callback",
			snapshot: valid,
			message:  "snapshot persistence callback is required",
		},
		{
			name: "invalid snapshot",
			snapshot: func() *state.SnapshotV2 {
				invalid := *valid
				invalid.Entries = nil
				return &invalid
			}(),
			persist: func(context.Context, *state.SnapshotV2) error { return nil },
			message: "entries are required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			applyState, err := newApplyStateV2(test.snapshot, test.persist)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, applyState)
		})
	}
}
