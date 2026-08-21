package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanDataSourceOperationClassifiesTargetPresence(t *testing.T) {
	desired := validPlannedDataSourceTarget(t)
	prior := operationDataSourceState(t)
	observed := operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-2"),
	})

	tests := []struct {
		name          string
		request       dataSourcePlanningRequest
		expected      *DataSourcePlanOperation
		expectedReads int
	}{
		{
			name: "absent",
		},
		{
			name: "initial read",
			request: dataSourcePlanningRequest{
				Desired: &desired,
			},
			expected: &DataSourcePlanOperation{
				Decision:        DecisionRead,
				Desired:         &desired,
				ObservedOutputs: &observed,
			},
			expectedReads: 1,
		},
		{
			name: "existing read",
			request: dataSourcePlanningRequest{
				Desired: &desired,
				Prior:   &prior,
			},
			expected: &DataSourcePlanOperation{
				Decision:        DecisionRead,
				Desired:         &desired,
				Prior:           &prior,
				ObservedOutputs: &observed,
			},
			expectedReads: 1,
		},
		{
			name: "destroy",
			request: dataSourcePlanningRequest{
				Prior: &prior,
			},
			expected: &DataSourcePlanOperation{
				Decision: DecisionDestroy,
				Prior:    &prior,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reads := 0
			operation, err := planDataSourceOperation(
				context.Background(),
				test.request,
				dataSourcePlanningCallbacks{
					Read: func(context.Context) (EncodedValue, error) {
						reads++
						return observed, nil
					},
				},
			)
			require.NoError(t, err)
			require.Equal(t, test.expected, operation)
			require.Equal(t, test.expectedReads, reads)
		})
	}
}

func TestPlanDataSourceOperationDefersPendingReads(t *testing.T) {
	prior := operationDataSourceState(t)

	tests := []struct {
		name    string
		desired func() PlannedDataSourceTarget
	}{
		{
			name: "pending inputs",
			desired: func() PlannedDataSourceTarget {
				desired := validPlannedDataSourceTarget(t)
				pending, err := PendingEncodedValue([]string{"resource.network.id"})
				require.NoError(t, err)
				desired.Inputs = operationObject(t, map[string]EncodedValue{
					"name": pending,
				})
				return desired
			},
		},
		{
			name: "pending configuration",
			desired: func() PlannedDataSourceTarget {
				desired := validPlannedDataSourceTarget(t)
				desired.Configuration = pendingOperationConfiguration()
				return desired
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			desired := test.desired()
			operation, err := planDataSourceOperation(
				context.Background(),
				dataSourcePlanningRequest{
					Desired: &desired,
					Prior:   &prior,
				},
				dataSourcePlanningCallbacks{},
			)
			require.NoError(t, err)
			require.Equal(t, &DataSourcePlanOperation{
				Decision: DecisionRead,
				Desired:  &desired,
				Prior:    &prior,
			}, operation)
		})
	}
}

func TestPlanDataSourceOperationRejectsInvalidRequestsBeforeReading(t *testing.T) {
	tests := []struct {
		name    string
		request func() dataSourcePlanningRequest
		message string
	}{
		{
			name: "desired",
			request: func() dataSourcePlanningRequest {
				desired := validPlannedDataSourceTarget(t)
				desired.Binding = Binding{}
				return dataSourcePlanningRequest{Desired: &desired}
			},
			message: "desired target: binding: library path is required",
		},
		{
			name: "prior",
			request: func() dataSourcePlanningRequest {
				prior := operationDataSourceState(t)
				prior.Outputs = StringValue("invalid")
				return dataSourcePlanningRequest{Prior: &prior}
			},
			message: "prior state: outputs must be an object",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			read := false
			operation, err := planDataSourceOperation(
				context.Background(),
				test.request(),
				dataSourcePlanningCallbacks{
					Read: func(context.Context) (EncodedValue, error) {
						read = true
						return EncodedValue{}, nil
					},
				},
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, operation)
			require.False(t, read)
		})
	}
}

func TestPlanDataSourceOperationRejectsReadFailures(t *testing.T) {
	desired := validPlannedDataSourceTarget(t)

	tests := []struct {
		name      string
		callbacks dataSourcePlanningCallbacks
		message   string
	}{
		{
			name:    "missing callback",
			message: "data-source read callback is required",
		},
		{
			name: "read error",
			callbacks: dataSourcePlanningCallbacks{
				Read: func(context.Context) (EncodedValue, error) {
					return EncodedValue{}, errors.New("unavailable")
				},
			},
			message: "read data source: unavailable",
		},
		{
			name: "read panic",
			callbacks: dataSourcePlanningCallbacks{
				Read: func(context.Context) (EncodedValue, error) {
					panic("provider defect")
				},
			},
			message: "panic in the library while reading this data source during planning",
		},
		{
			name: "invalid outputs",
			callbacks: dataSourcePlanningCallbacks{
				Read: func(context.Context) (EncodedValue, error) {
					return StringValue("invalid"), nil
				},
			},
			message: "data-source outputs must be an object",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation, err := planDataSourceOperation(
				context.Background(),
				dataSourcePlanningRequest{Desired: &desired},
				test.callbacks,
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, operation)
		})
	}
}

func TestPlanDataSourceOperationRequiresContext(t *testing.T) {
	var ctx context.Context
	operation, err := planDataSourceOperation(
		ctx,
		dataSourcePlanningRequest{},
		dataSourcePlanningCallbacks{},
	)
	require.ErrorContains(t, err, "data-source planning context is required")
	require.Nil(t, operation)
}

func TestPlanDataSourceOperationCopiesTargets(t *testing.T) {
	desired := validPlannedDataSourceTarget(t)
	desired.SensitiveInputPaths = []string{"/name"}
	desired.SensitiveOutputPaths = []string{"/id"}
	prior := operationDataSourceState(t)
	prior.DependsOn = []string{"resource.network"}
	prior.SensitiveInputPaths = []string{"/name"}
	prior.SensitiveOutputPaths = []string{"/id"}
	observed := operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-2"),
	})

	operation, err := planDataSourceOperation(
		context.Background(),
		dataSourcePlanningRequest{
			Desired: &desired,
			Prior:   &prior,
		},
		dataSourcePlanningCallbacks{
			Read: func(context.Context) (EncodedValue, error) {
				return observed, nil
			},
		},
	)
	require.NoError(t, err)
	require.NotSame(t, &desired, operation.Desired)
	require.NotSame(t, &prior, operation.Prior)
	require.NotSame(t, desired.Configuration.Record, operation.Desired.Configuration.Record)

	desired.SensitiveInputPaths[0] = "/changed"
	desired.Configuration.Record.SensitivePaths[0] = "/changed"
	prior.DependsOn[0] = "resource.changed"
	prior.SensitiveOutputPaths[0] = "/changed"
	prior.Configuration.SensitivePaths[0] = "/changed"

	require.Equal(t, []string{"/name"}, operation.Desired.SensitiveInputPaths)
	require.NotEqual(t, "/changed", operation.Desired.Configuration.Record.SensitivePaths[0])
	require.Equal(t, []string{"resource.network"}, operation.Prior.DependsOn)
	require.Equal(t, []string{"/id"}, operation.Prior.SensitiveOutputPaths)
	require.NotEqual(t, "/changed", operation.Prior.Configuration.SensitivePaths[0])

	pendingDesired := validPlannedDataSourceTarget(t)
	pendingDesired.Configuration = pendingOperationConfiguration()
	pendingOperation, err := planDataSourceOperation(
		context.Background(),
		dataSourcePlanningRequest{Desired: &pendingDesired},
		dataSourcePlanningCallbacks{},
	)
	require.NoError(t, err)
	pendingDesired.Configuration.PendingRefs[0] = "resource.changed.id"
	require.Equal(
		t,
		[]string{"resource.network.id"},
		pendingOperation.Desired.Configuration.PendingRefs,
	)
}
