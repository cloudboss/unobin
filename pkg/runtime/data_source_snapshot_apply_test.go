package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func newDataSourceApplySnapshot(
	t *testing.T,
	dataSources map[string]DataSourceStatePayload,
) *state.SnapshotV2 {
	t.Helper()
	snapshot := newRegisteredApplySnapshot(t, nil)
	for address, dataSource := range dataSources {
		require.NoError(t, snapshot.SetEntry(state.StateEntryV2{
			Address: address,
			Kind:    state.StateDataSource,
			Payload: state.StatePayload{
				Kind:       state.StateDataSource,
				DataSource: &dataSource,
			},
		}))
	}
	return snapshot
}

func dataSourceApplyStep(
	t *testing.T,
	address string,
	dependsOn []string,
	operation DataSourcePlanOperation,
) PlanStepV2 {
	t.Helper()
	step := PlanStepV2{
		Address:   address,
		Kind:      NodeDataSource,
		DependsOn: dependsOn,
		Operation: StepOperation{
			Kind:       StepDataSource,
			DataSource: &operation,
		},
	}
	require.NoError(t, step.Validate())
	return step
}

func TestApplyDataSourceSnapshotStepReadsAndPersistsState(t *testing.T) {
	desired := validPlannedDataSourceTarget(t)
	observed := operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-1"),
	})
	step := dataSourceApplyStep(
		t,
		"data-source.image",
		[]string{"resource.network"},
		DataSourcePlanOperation{
			Decision:        DecisionRead,
			Desired:         &desired,
			ObservedOutputs: &observed,
		},
	)
	original := newDataSourceApplySnapshot(t, nil)
	var persisted *state.SnapshotV2
	applyState, err := newApplyStateV2(
		original,
		func(_ context.Context, snapshot *state.SnapshotV2) error {
			persisted = snapshot
			return nil
		},
	)
	require.NoError(t, err)
	generatedAt := time.Date(2026, time.August, 20, 1, 0, 0, 0, time.UTC)
	applyState.now = func() time.Time { return generatedAt }
	reads := 0

	target, err := applyDataSourceSnapshotStep(
		context.Background(),
		applyState,
		dataSourceSnapshotApplyRequest{
			Step:    step,
			Desired: &desired,
			Read: func(context.Context) (EncodedValue, error) {
				reads++
				return observed, nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, reads)
	require.Empty(t, original.Entries)
	require.NotNil(t, persisted)
	require.Equal(t, generatedAt, persisted.GeneratedAt)

	expected := DataSourceStatePayload{
		Binding:              desired.Binding,
		Inputs:               desired.Inputs,
		Outputs:              observed,
		Configuration:        *desired.Configuration.Record,
		DependsOn:            []string{"resource.network"},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	require.Equal(t, &expected, target)
	entry := persisted.Find("data-source.image")
	require.NotNil(t, entry)
	require.Equal(t, state.StateDataSource, entry.Kind)
	require.Equal(t, target, entry.Payload.DataSource)

	persisted.Entries[0].Payload.DataSource.DependsOn[0] = "resource.changed"
	current, err := applyState.snapshotCopy()
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"resource.network"},
		current.Find("data-source.image").Payload.DataSource.DependsOn,
	)

	read, err := applyState.dataSourceState("data-source.image")
	require.NoError(t, err)
	read.DependsOn[0] = "resource.changed"
	readAgain, err := applyState.dataSourceState("data-source.image")
	require.NoError(t, err)
	require.Equal(t, []string{"resource.network"}, readAgain.DependsOn)
}

func TestApplyDataSourceSnapshotStepDestroysStateWithoutReading(t *testing.T) {
	prior := operationDataSourceState(t)
	step := dataSourceApplyStep(
		t,
		"data-source.image",
		[]string{},
		DataSourcePlanOperation{
			Decision: DecisionDestroy,
			Prior:    &prior,
		},
	)
	applyState, err := newApplyStateV2(
		newDataSourceApplySnapshot(t, map[string]DataSourceStatePayload{
			"data-source.image": prior,
		}),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)
	read := false

	target, err := applyDataSourceSnapshotStep(
		context.Background(),
		applyState,
		dataSourceSnapshotApplyRequest{
			Step: step,
			Read: func(context.Context) (EncodedValue, error) {
				read = true
				return EncodedValue{}, nil
			},
		},
	)
	require.NoError(t, err)
	require.Nil(t, target)
	require.False(t, read)
	current, err := applyState.snapshotCopy()
	require.NoError(t, err)
	require.Nil(t, current.Find("data-source.image"))
}

func TestApplyDataSourceSnapshotStepRejectsPriorStateMismatch(t *testing.T) {
	desired := validPlannedDataSourceTarget(t)
	observed := operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-1"),
	})
	plannedPrior := operationDataSourceState(t)
	changedPrior := plannedPrior
	changedPrior.Outputs = operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-changed"),
	})
	tests := []struct {
		name        string
		operation   DataSourcePlanOperation
		dataSources map[string]DataSourceStatePayload
		message     string
	}{
		{
			name: "missing",
			operation: DataSourcePlanOperation{
				Decision:        DecisionRead,
				Desired:         &desired,
				Prior:           &plannedPrior,
				ObservedOutputs: &observed,
			},
			message: "saved plan requires prior data-source state at data-source.image",
		},
		{
			name: "changed",
			operation: DataSourcePlanOperation{
				Decision:        DecisionRead,
				Desired:         &desired,
				Prior:           &plannedPrior,
				ObservedOutputs: &observed,
			},
			dataSources: map[string]DataSourceStatePayload{
				"data-source.image": changedPrior,
			},
			message: "prior data-source state does not match the saved plan",
		},
		{
			name: "unexpected",
			operation: DataSourcePlanOperation{
				Decision:        DecisionRead,
				Desired:         &desired,
				ObservedOutputs: &observed,
			},
			dataSources: map[string]DataSourceStatePayload{
				"data-source.image": plannedPrior,
			},
			message: "saved plan forbids prior data-source state at data-source.image",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			persisted := false
			applyState, err := newApplyStateV2(
				newDataSourceApplySnapshot(t, test.dataSources),
				func(context.Context, *state.SnapshotV2) error {
					persisted = true
					return nil
				},
			)
			require.NoError(t, err)
			read := false

			target, err := applyDataSourceSnapshotStep(
				context.Background(),
				applyState,
				dataSourceSnapshotApplyRequest{
					Step: dataSourceApplyStep(
						t,
						"data-source.image",
						[]string{},
						test.operation,
					),
					Desired: &desired,
					Read: func(context.Context) (EncodedValue, error) {
						read = true
						return observed, nil
					},
				},
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
			require.False(t, read)
			require.False(t, persisted)
		})
	}
}

func TestApplyDataSourceSnapshotStepKeepsStateOnPersistenceFailure(t *testing.T) {
	desired := validPlannedDataSourceTarget(t)
	observed := operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-1"),
	})
	step := dataSourceApplyStep(
		t,
		"data-source.image",
		[]string{},
		DataSourcePlanOperation{
			Decision:        DecisionRead,
			Desired:         &desired,
			ObservedOutputs: &observed,
		},
	)
	expectedErr := errors.New("state write failed")
	applyState, err := newApplyStateV2(
		newDataSourceApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error {
			return expectedErr
		},
	)
	require.NoError(t, err)
	before, err := applyState.snapshotCopy()
	require.NoError(t, err)
	read := false

	target, err := applyDataSourceSnapshotStep(
		context.Background(),
		applyState,
		dataSourceSnapshotApplyRequest{
			Step:    step,
			Desired: &desired,
			Read: func(context.Context) (EncodedValue, error) {
				read = true
				return observed, nil
			},
		},
	)
	require.ErrorIs(t, err, expectedErr)
	require.Nil(t, target)
	require.True(t, read)
	after, copyErr := applyState.snapshotCopy()
	require.NoError(t, copyErr)
	require.Equal(t, before, after)
}

func TestApplyDataSourceSnapshotStepRejectsInvalidSetup(t *testing.T) {
	desired := validPlannedDataSourceTarget(t)
	observed := operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-1"),
	})
	step := dataSourceApplyStep(
		t,
		"data-source.image",
		[]string{},
		DataSourcePlanOperation{
			Decision:        DecisionRead,
			Desired:         &desired,
			ObservedOutputs: &observed,
		},
	)
	applyState, err := newApplyStateV2(
		newDataSourceApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)
	request := dataSourceSnapshotApplyRequest{
		Step:    step,
		Desired: &desired,
		Read: func(context.Context) (EncodedValue, error) {
			return observed, nil
		},
	}
	var nilContext context.Context

	target, err := applyDataSourceSnapshotStep(nilContext, applyState, request)
	require.ErrorContains(t, err, "data-source apply context is required")
	require.Nil(t, target)
	target, err = applyDataSourceSnapshotStep(context.Background(), nil, request)
	require.ErrorContains(t, err, "version 2 apply state is required")
	require.Nil(t, target)
	output := OutputPlanOperation{
		Decision: DecisionEval,
		Value:    StringValue("value"),
	}
	request.Step = PlanStepV2{
		Address:   "output.result",
		Kind:      NodeOutput,
		DependsOn: []string{},
		Operation: StepOperation{
			Kind:   StepOutput,
			Output: &output,
		},
	}
	require.NoError(t, request.Step.Validate())
	target, err = applyDataSourceSnapshotStep(context.Background(), applyState, request)
	require.ErrorContains(t, err, "saved data-source step must be a data source")
	require.Nil(t, target)
}

func TestApplyStateV2RejectsNonDataSourceEntryForDataSourceAddress(t *testing.T) {
	snapshot := newDataSourceApplySnapshot(t, nil)
	composite := operationCompositeState(t, NodeDataSource)
	require.NoError(t, snapshot.SetEntry(state.StateEntryV2{
		Address: "data-source.image",
		Kind:    state.StateComposite,
		Payload: state.StatePayload{
			Kind:      state.StateComposite,
			Composite: &composite,
		},
	}))
	applyState, err := newApplyStateV2(
		snapshot,
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)

	dataSource, err := applyState.dataSourceState("data-source.image")
	require.ErrorContains(t, err, "state entry data-source.image is not a data source")
	require.Nil(t, dataSource)
}
