package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanLibraryConfigurationOperationEvaluatesConcreteInputs(t *testing.T) {
	inputs := operationObject(t, map[string]EncodedValue{
		"region": StringValue("east"),
	})
	evaluated := validConfigurationRecord(t)

	operation, err := planLibraryConfigurationOperation(
		context.Background(),
		libraryConfigurationPlanningRequest{Inputs: inputs},
		libraryConfigurationPlanningCallbacks{
			Eval: func(_ context.Context, got EncodedValue) (ConfigurationRecord, error) {
				require.Equal(t, inputs, got)
				return evaluated, nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, &LibraryConfigurationPlanOperation{
		Decision: DecisionEval,
		Inputs:   inputs,
		Result: PlannedConfiguration{
			Kind:   PlannedConfigurationConcrete,
			Record: &evaluated,
		},
	}, operation)

	evaluated.SensitivePaths[0] = "/changed"
	require.NotEqual(t, "/changed", operation.Result.Record.SensitivePaths[0])
}

func TestPlanLibraryConfigurationOperationDefersPendingInputs(t *testing.T) {
	first, err := PendingEncodedValue([]string{
		"data-source.image.id",
		"resource.network.id",
	})
	require.NoError(t, err)
	second, err := PendingEncodedValue([]string{
		"action.prepare.result",
		"resource.network.id",
	})
	require.NoError(t, err)
	inputs := operationObject(t, map[string]EncodedValue{
		"endpoint": first,
		"token":    second,
	})

	operation, err := planLibraryConfigurationOperation(
		context.Background(),
		libraryConfigurationPlanningRequest{Inputs: inputs},
		libraryConfigurationPlanningCallbacks{},
	)
	require.NoError(t, err)
	require.Equal(t, &LibraryConfigurationPlanOperation{
		Decision: DecisionEval,
		Inputs:   inputs,
		Result: PlannedConfiguration{
			Kind: PlannedConfigurationPending,
			PendingRefs: []string{
				"action.prepare.result",
				"data-source.image.id",
				"resource.network.id",
			},
		},
	}, operation)
}

func TestPlanLibraryConfigurationOperationRejectsInvalidInputsBeforeEvaluation(t *testing.T) {
	evaluated := false
	operation, err := planLibraryConfigurationOperation(
		context.Background(),
		libraryConfigurationPlanningRequest{Inputs: StringValue("invalid")},
		libraryConfigurationPlanningCallbacks{
			Eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
				evaluated = true
				return validConfigurationRecord(t), nil
			},
		},
	)
	require.ErrorContains(t, err, "library-configuration inputs must be an object")
	require.Nil(t, operation)
	require.False(t, evaluated)
}

func TestPlanLibraryConfigurationOperationRejectsEvaluationFailures(t *testing.T) {
	inputs := operationObject(t, map[string]EncodedValue{
		"region": StringValue("east"),
	})
	expectedErr := errors.New("evaluation failed")

	tests := []struct {
		name      string
		callbacks libraryConfigurationPlanningCallbacks
		message   string
	}{
		{
			name:    "missing callback",
			message: "library-configuration evaluator is required",
		},
		{
			name: "evaluation error",
			callbacks: libraryConfigurationPlanningCallbacks{
				Eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
					return ConfigurationRecord{}, expectedErr
				},
			},
			message: "evaluate library configuration: evaluation failed",
		},
		{
			name: "evaluation panic",
			callbacks: libraryConfigurationPlanningCallbacks{
				Eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
					panic("library defect")
				},
			},
			message: "panic in the library while evaluating this library configuration",
		},
		{
			name: "invalid result",
			callbacks: libraryConfigurationPlanningCallbacks{
				Eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
					return ConfigurationRecord{}, nil
				},
			},
			message: "evaluated library configuration: library path is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation, err := planLibraryConfigurationOperation(
				context.Background(),
				libraryConfigurationPlanningRequest{Inputs: inputs},
				test.callbacks,
			)
			require.ErrorContains(t, err, test.message)
			if test.name == "evaluation error" {
				require.ErrorIs(t, err, expectedErr)
			}
			require.Nil(t, operation)
		})
	}
}

func TestPlanLibraryConfigurationOperationRequiresContext(t *testing.T) {
	var ctx context.Context
	operation, err := planLibraryConfigurationOperation(
		ctx,
		libraryConfigurationPlanningRequest{},
		libraryConfigurationPlanningCallbacks{},
	)
	require.ErrorContains(t, err, "library-configuration planning context is required")
	require.Nil(t, operation)
}
