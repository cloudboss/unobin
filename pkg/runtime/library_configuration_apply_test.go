package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyLibraryConfigurationOperationEvaluatesConcreteResult(t *testing.T) {
	inputs := operationObject(t, map[string]EncodedValue{
		"region": StringValue("east"),
	})
	planned := validConfigurationRecord(t)
	request := libraryConfigurationApplyRequest{
		Address: "library-config.cloud",
		Operation: LibraryConfigurationPlanOperation{
			Decision: DecisionEval,
			Inputs:   inputs,
			Result: PlannedConfiguration{
				Kind:   PlannedConfigurationConcrete,
				Record: &planned,
			},
		},
		Inputs: inputs,
	}
	evaluated := false
	current := cloneConfigurationRecord(planned)

	result, err := applyLibraryConfigurationOperation(
		context.Background(),
		request,
		libraryConfigurationApplyCallbacks{
			Eval: func(_ context.Context, got EncodedValue) (ConfigurationRecord, error) {
				evaluated = true
				require.Equal(t, inputs, got)
				return current, nil
			},
		},
	)
	require.NoError(t, err)
	require.True(t, evaluated)
	require.Equal(t, &planned, result)

	current.SensitivePaths[0] = "/changed"
	require.Equal(t, planned.SensitivePaths, result.SensitivePaths)
}

func TestApplyLibraryConfigurationOperationAcceptsResolvedPendingResult(t *testing.T) {
	pending, err := PendingEncodedValue([]string{"resource.network.id"})
	require.NoError(t, err)
	plannedInputs := operationObject(t, map[string]EncodedValue{
		"endpoint": pending,
		"region":   StringValue("east"),
	})
	currentInputs := operationObject(t, map[string]EncodedValue{
		"endpoint": StringValue("network-1"),
		"region":   StringValue("east"),
	})
	current := validConfigurationRecord(t)

	result, err := applyLibraryConfigurationOperation(
		context.Background(),
		libraryConfigurationApplyRequest{
			Address: "library-config.cloud",
			Operation: LibraryConfigurationPlanOperation{
				Decision: DecisionEval,
				Inputs:   plannedInputs,
				Result:   pendingOperationConfiguration(),
			},
			Inputs: currentInputs,
		},
		libraryConfigurationApplyCallbacks{
			Eval: func(_ context.Context, got EncodedValue) (ConfigurationRecord, error) {
				require.Equal(t, currentInputs, got)
				return current, nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, &current, result)
}

func TestApplyLibraryConfigurationOperationRejectsChangedInputs(t *testing.T) {
	pending, err := PendingEncodedValue([]string{"resource.network.id"})
	require.NoError(t, err)
	plannedInputs := operationObject(t, map[string]EncodedValue{
		"endpoint": pending,
		"region":   StringValue("east"),
	})
	plannedResult := pendingOperationConfiguration()

	tests := []struct {
		name    string
		inputs  EncodedValue
		message string
	}{
		{
			name: "known input changed",
			inputs: operationObject(t, map[string]EncodedValue{
				"endpoint": StringValue("network-1"),
				"region":   StringValue("west"),
			}),
			message: "inputs do not match the saved plan",
		},
		{
			name: "input remains pending",
			inputs: operationObject(t, map[string]EncodedValue{
				"endpoint": pending,
				"region":   StringValue("east"),
			}),
			message: "current library-configuration inputs must be concrete",
		},
		{
			name:    "inputs are not an object",
			inputs:  StringValue("bad"),
			message: "current library-configuration inputs must be an object",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evaluated := false
			result, err := applyLibraryConfigurationOperation(
				context.Background(),
				libraryConfigurationApplyRequest{
					Address: "library-config.cloud",
					Operation: LibraryConfigurationPlanOperation{
						Decision: DecisionEval,
						Inputs:   plannedInputs,
						Result:   plannedResult,
					},
					Inputs: test.inputs,
				},
				libraryConfigurationApplyCallbacks{
					Eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
						evaluated = true
						return validConfigurationRecord(t), nil
					},
				},
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, result)
			require.False(t, evaluated)
		})
	}
}

func TestApplyLibraryConfigurationOperationRejectsChangedConcreteResult(t *testing.T) {
	inputs := operationObject(t, map[string]EncodedValue{
		"region": StringValue("east"),
	})
	planned := validConfigurationRecord(t)
	current := cloneConfigurationRecord(planned)
	current.Address = "library-config.other"

	result, err := applyLibraryConfigurationOperation(
		context.Background(),
		libraryConfigurationApplyRequest{
			Address: "library-config.cloud",
			Operation: LibraryConfigurationPlanOperation{
				Decision: DecisionEval,
				Inputs:   inputs,
				Result: PlannedConfiguration{
					Kind:   PlannedConfigurationConcrete,
					Record: &planned,
				},
			},
			Inputs: inputs,
		},
		libraryConfigurationApplyCallbacks{
			Eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
				return current, nil
			},
		},
	)
	require.ErrorContains(t, err, "result changed since the plan was computed")
	require.Nil(t, result)
}

func TestApplyLibraryConfigurationOperationRejectsInvalidSetup(t *testing.T) {
	inputs := operationObject(t, map[string]EncodedValue{
		"region": StringValue("east"),
	})
	planned := validConfigurationRecord(t)
	request := libraryConfigurationApplyRequest{
		Address: "library-config.cloud",
		Operation: LibraryConfigurationPlanOperation{
			Decision: DecisionEval,
			Inputs:   inputs,
			Result: PlannedConfiguration{
				Kind:   PlannedConfigurationConcrete,
				Record: &planned,
			},
		},
		Inputs: inputs,
	}
	callbacks := libraryConfigurationApplyCallbacks{
		Eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
			return planned, nil
		},
	}
	var nilContext context.Context

	result, err := applyLibraryConfigurationOperation(nilContext, request, callbacks)
	require.ErrorContains(t, err, "apply context is required")
	require.Nil(t, result)

	request.Address = "resource.cloud"
	result, err = applyLibraryConfigurationOperation(context.Background(), request, callbacks)
	require.ErrorContains(t, err, "library-configuration address is invalid")
	require.Nil(t, result)

	request.Address = "library-config.cloud"
	request.Operation.Decision = DecisionRead
	result, err = applyLibraryConfigurationOperation(context.Background(), request, callbacks)
	require.ErrorContains(t, err, "saved library-configuration operation")
	require.Nil(t, result)

	request.Operation.Decision = DecisionEval
	callbacks.Eval = nil
	result, err = applyLibraryConfigurationOperation(context.Background(), request, callbacks)
	require.ErrorContains(t, err, "library-configuration evaluator is required")
	require.Nil(t, result)
}

func TestApplyLibraryConfigurationOperationReportsEvaluationFailures(t *testing.T) {
	inputs := operationObject(t, map[string]EncodedValue{
		"region": StringValue("east"),
	})
	planned := validConfigurationRecord(t)
	request := libraryConfigurationApplyRequest{
		Address: "library-config.cloud",
		Operation: LibraryConfigurationPlanOperation{
			Decision: DecisionEval,
			Inputs:   inputs,
			Result: PlannedConfiguration{
				Kind:   PlannedConfigurationConcrete,
				Record: &planned,
			},
		},
		Inputs: inputs,
	}
	expectedErr := errors.New("evaluation failed")

	tests := []struct {
		name    string
		eval    func(context.Context, EncodedValue) (ConfigurationRecord, error)
		message string
	}{
		{
			name: "evaluation error",
			eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
				return ConfigurationRecord{}, expectedErr
			},
			message: "evaluate library configuration",
		},
		{
			name: "evaluation panic",
			eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
				panic("failed")
			},
			message: "panic in the library while evaluating this library configuration",
		},
		{
			name: "invalid result",
			eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
				return ConfigurationRecord{}, nil
			},
			message: "evaluated library configuration: library path is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := applyLibraryConfigurationOperation(
				context.Background(),
				request,
				libraryConfigurationApplyCallbacks{Eval: test.eval},
			)
			require.ErrorContains(t, err, test.message)
			if test.name == "evaluation error" {
				require.ErrorIs(t, err, expectedErr)
			}
			require.Nil(t, result)
		})
	}
}
