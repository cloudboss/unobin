package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyDataSourceOperationReadsAndPersistsState(t *testing.T) {
	desired := validPlannedDataSourceTarget(t)
	observed := operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-1"),
	})
	request := dataSourceApplyRequest{
		Address: "data-source.image",
		Operation: DataSourcePlanOperation{
			Decision:        DecisionRead,
			Desired:         &desired,
			ObservedOutputs: &observed,
		},
		Desired:   &desired,
		DependsOn: []string{"resource.network"},
	}
	reads := 0
	var persisted *DataSourceStatePayload

	target, err := applyDataSourceOperation(
		context.Background(),
		request,
		dataSourceApplyCallbacks{
			Read: func(context.Context) (EncodedValue, error) {
				reads++
				return observed, nil
			},
			Persist: func(_ context.Context, target *DataSourceStatePayload) error {
				persisted = target
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, reads)
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
	require.Equal(t, target, persisted)

	request.DependsOn[0] = "resource.changed"
	require.Equal(t, []string{"resource.network"}, target.DependsOn)
}

func TestApplyDataSourceOperationAcceptsResolvedPendingTarget(t *testing.T) {
	pending, err := PendingEncodedValue([]string{"resource.network.id"})
	require.NoError(t, err)
	planned := validPlannedDataSourceTarget(t)
	planned.Inputs = operationObject(t, map[string]EncodedValue{"name": pending})
	planned.Configuration = pendingOperationConfiguration()
	current := validPlannedDataSourceTarget(t)
	outputs := operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-2"),
	})
	var persisted *DataSourceStatePayload

	target, err := applyDataSourceOperation(
		context.Background(),
		dataSourceApplyRequest{
			Address: "data-source.image",
			Operation: DataSourcePlanOperation{
				Decision: DecisionRead,
				Desired:  &planned,
			},
			Desired:   &current,
			DependsOn: []string{},
		},
		dataSourceApplyCallbacks{
			Read: func(context.Context) (EncodedValue, error) {
				return outputs, nil
			},
			Persist: func(_ context.Context, target *DataSourceStatePayload) error {
				persisted = target
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, outputs, target.Outputs)
	require.Equal(t, current.Inputs, target.Inputs)
	require.Equal(t, current.Configuration.Record, &target.Configuration)
	require.Equal(t, target, persisted)
}

func TestApplyDataSourceOperationRejectsChangedObservedOutputs(t *testing.T) {
	desired := validPlannedDataSourceTarget(t)
	planned := operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-1"),
	})
	current := operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-2"),
	})
	read := false
	persisted := false

	target, err := applyDataSourceOperation(
		context.Background(),
		dataSourceApplyRequest{
			Address: "data-source.image",
			Operation: DataSourcePlanOperation{
				Decision:        DecisionRead,
				Desired:         &desired,
				ObservedOutputs: &planned,
			},
			Desired:   &desired,
			DependsOn: []string{},
		},
		dataSourceApplyCallbacks{
			Read: func(context.Context) (EncodedValue, error) {
				read = true
				return current, nil
			},
			Persist: func(context.Context, *DataSourceStatePayload) error {
				persisted = true
				return nil
			},
		},
	)
	require.ErrorContains(t, err, "data source outputs changed since the plan was computed")
	require.Nil(t, target)
	require.True(t, read)
	require.False(t, persisted)
}

func TestApplyDataSourceOperationDestroysStateWithoutReading(t *testing.T) {
	prior := operationDataSourceState(t)
	read := false
	persisted := false

	target, err := applyDataSourceOperation(
		context.Background(),
		dataSourceApplyRequest{
			Address: "data-source.image",
			Operation: DataSourcePlanOperation{
				Decision: DecisionDestroy,
				Prior:    &prior,
			},
			Prior:     &prior,
			DependsOn: []string{},
		},
		dataSourceApplyCallbacks{
			Read: func(context.Context) (EncodedValue, error) {
				read = true
				return EncodedValue{}, nil
			},
			Persist: func(_ context.Context, target *DataSourceStatePayload) error {
				persisted = true
				require.Nil(t, target)
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Nil(t, target)
	require.False(t, read)
	require.True(t, persisted)
}

func TestApplyDataSourceOperationRejectsChangedPremises(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*dataSourceApplyRequest, *dataSourceApplyCallbacks)
		message string
	}{
		{
			name: "missing desired target",
			change: func(request *dataSourceApplyRequest, _ *dataSourceApplyCallbacks) {
				request.Desired = nil
			},
			message: "saved operation requires a desired data source",
		},
		{
			name: "known input changed",
			change: func(request *dataSourceApplyRequest, _ *dataSourceApplyCallbacks) {
				request.Desired.Inputs = operationObject(t, map[string]EncodedValue{
					"name": StringValue("other"),
				})
			},
			message: "desired inputs do not match the saved plan",
		},
		{
			name: "binding changed",
			change: func(request *dataSourceApplyRequest, _ *dataSourceApplyCallbacks) {
				request.Desired.Binding.Export = "archive"
			},
			message: "desired binding does not match the saved plan",
		},
		{
			name: "current input remains pending",
			change: func(request *dataSourceApplyRequest, _ *dataSourceApplyCallbacks) {
				pending, err := PendingEncodedValue([]string{"resource.network.id"})
				require.NoError(t, err)
				request.Desired.Inputs = operationObject(t, map[string]EncodedValue{
					"name": pending,
				})
			},
			message: "desired inputs did not resolve before apply",
		},
		{
			name: "configuration remains pending",
			change: func(request *dataSourceApplyRequest, _ *dataSourceApplyCallbacks) {
				request.Desired.Configuration = pendingOperationConfiguration()
			},
			message: "desired configuration did not resolve before apply",
		},
		{
			name: "configuration changed",
			change: func(request *dataSourceApplyRequest, _ *dataSourceApplyCallbacks) {
				record := *request.Desired.Configuration.Record
				record.Address = "library-config.changed"
				request.Desired.Configuration.Record = &record
			},
			message: "desired configuration does not match the saved plan",
		},
		{
			name: "sensitive paths changed",
			change: func(request *dataSourceApplyRequest, _ *dataSourceApplyCallbacks) {
				request.Desired.SensitiveInputPaths = []string{"/name"}
			},
			message: "sensitive input paths do not match the saved plan",
		},
		{
			name: "unexpected prior state",
			change: func(request *dataSourceApplyRequest, _ *dataSourceApplyCallbacks) {
				prior := operationDataSourceState(t)
				request.Prior = &prior
			},
			message: "saved operation forbids prior data-source state",
		},
		{
			name: "missing prior state",
			change: func(request *dataSourceApplyRequest, _ *dataSourceApplyCallbacks) {
				prior := operationDataSourceState(t)
				request.Operation.Prior = &prior
			},
			message: "saved operation requires prior data-source state",
		},
		{
			name: "changed prior state",
			change: func(request *dataSourceApplyRequest, _ *dataSourceApplyCallbacks) {
				planned := operationDataSourceState(t)
				current := planned
				current.Outputs = operationObject(t, map[string]EncodedValue{
					"id": StringValue("ami-2"),
				})
				request.Operation.Prior = &planned
				request.Prior = &current
			},
			message: "prior data-source state does not match the saved plan",
		},
		{
			name: "destroy no longer absent",
			change: func(request *dataSourceApplyRequest, _ *dataSourceApplyCallbacks) {
				prior := operationDataSourceState(t)
				request.Operation = DataSourcePlanOperation{
					Decision: DecisionDestroy,
					Prior:    &prior,
				}
				request.Prior = &prior
			},
			message: "saved destroy requires the data source to remain absent",
		},
		{
			name: "missing dependencies",
			change: func(request *dataSourceApplyRequest, _ *dataSourceApplyCallbacks) {
				request.DependsOn = nil
			},
			message: "dependencies are required",
		},
		{
			name: "missing read callback",
			change: func(_ *dataSourceApplyRequest, callbacks *dataSourceApplyCallbacks) {
				callbacks.Read = nil
			},
			message: "data-source read callback is required",
		},
		{
			name: "missing persistence callback",
			change: func(_ *dataSourceApplyRequest, callbacks *dataSourceApplyCallbacks) {
				callbacks.Persist = nil
			},
			message: "data-source state persistence callback is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plannedDesired := validPlannedDataSourceTarget(t)
			currentDesired := plannedDesired
			observed := operationObject(t, map[string]EncodedValue{
				"id": StringValue("ami-1"),
			})
			request := dataSourceApplyRequest{
				Address: "data-source.image",
				Operation: DataSourcePlanOperation{
					Decision:        DecisionRead,
					Desired:         &plannedDesired,
					ObservedOutputs: &observed,
				},
				Desired:   &currentDesired,
				DependsOn: []string{},
			}
			read := false
			persisted := false
			callbacks := dataSourceApplyCallbacks{
				Read: func(context.Context) (EncodedValue, error) {
					read = true
					return observed, nil
				},
				Persist: func(context.Context, *DataSourceStatePayload) error {
					persisted = true
					return nil
				},
			}
			test.change(&request, &callbacks)

			target, err := applyDataSourceOperation(context.Background(), request, callbacks)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
			require.False(t, read)
			require.False(t, persisted)
		})
	}
}

func TestApplyDataSourceOperationRejectsInvalidSetup(t *testing.T) {
	desired := validPlannedDataSourceTarget(t)
	observed := operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-1"),
	})
	request := dataSourceApplyRequest{
		Address: "data-source.image",
		Operation: DataSourcePlanOperation{
			Decision:        DecisionRead,
			Desired:         &desired,
			ObservedOutputs: &observed,
		},
		Desired:   &desired,
		DependsOn: []string{},
	}
	callbacks := dataSourceApplyCallbacks{
		Read: func(context.Context) (EncodedValue, error) {
			return observed, nil
		},
		Persist: func(context.Context, *DataSourceStatePayload) error { return nil },
	}
	var nilContext context.Context

	target, err := applyDataSourceOperation(nilContext, request, callbacks)
	require.ErrorContains(t, err, "apply context is required")
	require.Nil(t, target)
	request.Address = "resource.image"
	target, err = applyDataSourceOperation(context.Background(), request, callbacks)
	require.ErrorContains(t, err, "address category resource does not match data-source")
	require.Nil(t, target)
}

func TestApplyDataSourceOperationReportsExecutionFailures(t *testing.T) {
	expectedErr := errors.New("operation failed")
	tests := []struct {
		name       string
		read       func(context.Context) (EncodedValue, error)
		persistErr error
		message    string
	}{
		{
			name: "read error",
			read: func(context.Context) (EncodedValue, error) {
				return EncodedValue{}, expectedErr
			},
			message: "read data source",
		},
		{
			name: "read panic",
			read: func(context.Context) (EncodedValue, error) {
				panic("failed")
			},
			message: "panic in the library while reading this data source",
		},
		{
			name: "non-object outputs",
			read: func(context.Context) (EncodedValue, error) {
				return StringValue("bad"), nil
			},
			message: "data-source outputs must be an object",
		},
		{
			name: "persistence error",
			read: func(context.Context) (EncodedValue, error) {
				return operationObject(t, map[string]EncodedValue{
					"id": StringValue("ami-1"),
				}), nil
			},
			persistErr: expectedErr,
			message:    "persist data-source state",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			desired := validPlannedDataSourceTarget(t)
			observed := operationObject(t, map[string]EncodedValue{
				"id": StringValue("ami-1"),
			})
			request := dataSourceApplyRequest{
				Address: "data-source.image",
				Operation: DataSourcePlanOperation{
					Decision:        DecisionRead,
					Desired:         &desired,
					ObservedOutputs: &observed,
				},
				Desired:   &desired,
				DependsOn: []string{},
			}
			persisted := false

			target, err := applyDataSourceOperation(
				context.Background(),
				request,
				dataSourceApplyCallbacks{
					Read: test.read,
					Persist: func(context.Context, *DataSourceStatePayload) error {
						persisted = true
						return test.persistErr
					},
				},
			)
			require.ErrorContains(t, err, test.message)
			if test.message == "read data source" || test.persistErr != nil {
				require.ErrorIs(t, err, expectedErr)
			}
			require.Nil(t, target)
			require.Equal(t, test.persistErr != nil, persisted)
		})
	}
}
