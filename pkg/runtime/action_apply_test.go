package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyActionOperationRerunsResolvedTarget(t *testing.T) {
	pending, err := PendingEncodedValue([]string{"resource.service.id"})
	require.NoError(t, err)
	planned := validPlannedActionTarget(t)
	planned.Inputs = operationObject(t, map[string]EncodedValue{
		"message": pending,
	})
	planned.Configuration = pendingOperationConfiguration()
	planned.TriggerHash = ""
	current := validPlannedActionTarget(t)
	outputs := operationObject(t, map[string]EncodedValue{
		"sent": BooleanValue(true),
	})
	request := actionApplyRequest{
		Address: "action.notify",
		Operation: ActionPlanOperation{
			Decision: DecisionRerun,
			Desired:  &planned,
		},
		Desired:   &current,
		DependsOn: []string{"resource.service"},
	}
	var events []string
	var persisted *ActionStatePayload

	target, err := applyActionOperation(
		context.Background(),
		request,
		actionApplyCallbacks{
			Run: func(context.Context) (EncodedValue, error) {
				events = append(events, "run")
				return outputs, nil
			},
			Persist: func(_ context.Context, target *ActionStatePayload) error {
				events = append(events, "persist")
				persisted = target
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"run", "persist"}, events)
	expected := ActionStatePayload{
		Binding:              current.Binding,
		Inputs:               current.Inputs,
		Outputs:              outputs,
		Configuration:        *current.Configuration.Record,
		TriggerHash:          current.TriggerHash,
		DependsOn:            []string{"resource.service"},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	require.Equal(t, &expected, target)
	require.Equal(t, target, persisted)
}

func TestApplyActionOperationSkipsWithoutRunning(t *testing.T) {
	desired := validPlannedActionTarget(t)
	desired.Inputs = operationObject(t, map[string]EncodedValue{
		"message": StringValue("updated"),
	})
	prior := operationActionState(t)
	request := actionApplyRequest{
		Address: "action.notify",
		Operation: ActionPlanOperation{
			Decision: DecisionSkip,
			Desired:  &desired,
			Prior:    &prior,
		},
		Desired:   &desired,
		Prior:     &prior,
		DependsOn: []string{"resource.service"},
	}
	run := false
	var persisted *ActionStatePayload

	target, err := applyActionOperation(
		context.Background(),
		request,
		actionApplyCallbacks{
			Run: func(context.Context) (EncodedValue, error) {
				run = true
				return EncodedValue{}, nil
			},
			Persist: func(_ context.Context, target *ActionStatePayload) error {
				persisted = target
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.False(t, run)
	require.NotNil(t, target)
	require.Equal(t, desired.Inputs, target.Inputs)
	require.Equal(t, prior.Outputs, target.Outputs)
	require.Equal(t, []string{"resource.service"}, target.DependsOn)
	require.Equal(t, target, persisted)
}

func TestApplyActionOperationDestroysStateWithoutRunning(t *testing.T) {
	prior := operationActionState(t)
	request := actionApplyRequest{
		Address: "action.notify",
		Operation: ActionPlanOperation{
			Decision: DecisionDestroy,
			Prior:    &prior,
		},
		Prior:     &prior,
		DependsOn: []string{},
	}
	run := false
	persisted := false

	target, err := applyActionOperation(
		context.Background(),
		request,
		actionApplyCallbacks{
			Run: func(context.Context) (EncodedValue, error) {
				run = true
				return EncodedValue{}, nil
			},
			Persist: func(_ context.Context, target *ActionStatePayload) error {
				persisted = true
				require.Nil(t, target)
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Nil(t, target)
	require.False(t, run)
	require.True(t, persisted)
}

func TestApplyActionOperationRejectsChangedPremises(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*actionApplyRequest, *actionApplyCallbacks)
		message string
	}{
		{
			name: "missing desired target",
			change: func(request *actionApplyRequest, _ *actionApplyCallbacks) {
				request.Desired = nil
			},
			message: "saved operation requires a desired action",
		},
		{
			name: "known input changed",
			change: func(request *actionApplyRequest, _ *actionApplyCallbacks) {
				request.Desired.Inputs = operationObject(t, map[string]EncodedValue{
					"message": StringValue("changed"),
				})
			},
			message: "desired inputs do not match the saved plan",
		},
		{
			name: "current input remains pending",
			change: func(request *actionApplyRequest, _ *actionApplyCallbacks) {
				pending, err := PendingEncodedValue([]string{"resource.service.id"})
				require.NoError(t, err)
				request.Desired.Inputs = operationObject(t, map[string]EncodedValue{
					"message": pending,
				})
			},
			message: "desired inputs did not resolve before apply",
		},
		{
			name: "configuration changed",
			change: func(request *actionApplyRequest, _ *actionApplyCallbacks) {
				record := differentActionConfigurationRecord(t)
				request.Desired.Configuration.Record = &record
			},
			message: "desired configuration does not match the saved plan",
		},
		{
			name: "trigger changed",
			change: func(request *actionApplyRequest, _ *actionApplyCallbacks) {
				request.Desired.TriggerHash = "changed"
			},
			message: "trigger hash does not match the saved plan",
		},
		{
			name: "sensitive paths changed",
			change: func(request *actionApplyRequest, _ *actionApplyCallbacks) {
				request.Desired.SensitiveInputPaths = []string{"/message"}
			},
			message: "sensitive input paths do not match the saved plan",
		},
		{
			name: "prior state missing",
			change: func(request *actionApplyRequest, _ *actionApplyCallbacks) {
				request.Prior = nil
			},
			message: "saved operation requires prior action state",
		},
		{
			name: "prior state changed",
			change: func(request *actionApplyRequest, _ *actionApplyCallbacks) {
				request.Prior.TriggerHash = "changed"
			},
			message: "prior action state does not match the saved plan",
		},
		{
			name: "missing run callback",
			change: func(_ *actionApplyRequest, callbacks *actionApplyCallbacks) {
				callbacks.Run = nil
			},
			message: "action run callback is required",
		},
		{
			name: "missing persistence callback",
			change: func(_ *actionApplyRequest, callbacks *actionApplyCallbacks) {
				callbacks.Persist = nil
			},
			message: "action state persistence callback is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plannedDesired := validPlannedActionTarget(t)
			currentDesired := plannedDesired
			plannedPrior := operationActionState(t)
			currentPrior := plannedPrior
			request := actionApplyRequest{
				Address: "action.notify",
				Operation: ActionPlanOperation{
					Decision: DecisionRerun,
					Desired:  &plannedDesired,
					Prior:    &plannedPrior,
				},
				Desired:   &currentDesired,
				Prior:     &currentPrior,
				DependsOn: []string{},
			}
			run := false
			persisted := false
			callbacks := actionApplyCallbacks{
				Run: func(context.Context) (EncodedValue, error) {
					run = true
					return operationObject(t, map[string]EncodedValue{}), nil
				},
				Persist: func(context.Context, *ActionStatePayload) error {
					persisted = true
					return nil
				},
			}
			test.change(&request, &callbacks)

			target, err := applyActionOperation(context.Background(), request, callbacks)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
			require.False(t, run)
			require.False(t, persisted)
		})
	}
}

func TestApplyActionOperationRejectsInvalidSetup(t *testing.T) {
	desired := validPlannedActionTarget(t)
	request := actionApplyRequest{
		Address: "action.notify",
		Operation: ActionPlanOperation{
			Decision: DecisionRerun,
			Desired:  &desired,
		},
		Desired:   &desired,
		DependsOn: []string{},
	}
	callbacks := actionApplyCallbacks{
		Run: func(context.Context) (EncodedValue, error) {
			return operationObject(t, map[string]EncodedValue{}), nil
		},
		Persist: func(context.Context, *ActionStatePayload) error { return nil },
	}
	var nilContext context.Context

	target, err := applyActionOperation(nilContext, request, callbacks)
	require.ErrorContains(t, err, "apply context is required")
	require.Nil(t, target)
	request.Address = "resource.notify"
	target, err = applyActionOperation(context.Background(), request, callbacks)
	require.ErrorContains(t, err, "address category resource does not match action")
	require.Nil(t, target)
}

func TestApplyActionOperationReportsExecutionFailures(t *testing.T) {
	expectedErr := errors.New("operation failed")
	tests := []struct {
		name       string
		run        func(context.Context) (EncodedValue, error)
		persistErr error
		message    string
	}{
		{
			name: "run error",
			run: func(context.Context) (EncodedValue, error) {
				return EncodedValue{}, expectedErr
			},
			message: "run action",
		},
		{
			name: "run panic",
			run: func(context.Context) (EncodedValue, error) {
				panic("failed")
			},
			message: "panic in the library while running this action",
		},
		{
			name: "non-object outputs",
			run: func(context.Context) (EncodedValue, error) {
				return StringValue("bad"), nil
			},
			message: "action outputs must be an object",
		},
		{
			name: "persistence error",
			run: func(context.Context) (EncodedValue, error) {
				return operationObject(t, map[string]EncodedValue{}), nil
			},
			persistErr: expectedErr,
			message:    "persist action state",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			desired := validPlannedActionTarget(t)
			request := actionApplyRequest{
				Address: "action.notify",
				Operation: ActionPlanOperation{
					Decision: DecisionRerun,
					Desired:  &desired,
				},
				Desired:   &desired,
				DependsOn: []string{},
			}
			persisted := false

			target, err := applyActionOperation(
				context.Background(),
				request,
				actionApplyCallbacks{
					Run: test.run,
					Persist: func(context.Context, *ActionStatePayload) error {
						persisted = true
						return test.persistErr
					},
				},
			)
			require.ErrorContains(t, err, test.message)
			if test.persistErr != nil || test.run != nil && test.message == "run action" {
				require.ErrorIs(t, err, expectedErr)
			}
			require.Nil(t, target)
			require.Equal(t, test.persistErr != nil, persisted)
		})
	}
}

func differentActionConfigurationRecord(t *testing.T) ConfigurationRecord {
	t.Helper()
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	record, err := definition.newConfigurationRecordWith(
		"library-config.cloud",
		testConfigurationValue(t),
		[]string{"/servers/1", "/credentials", "/nullable", "/labels/a~1b~0"},
		nil,
		deterministicIDs(
			"55555555555555555555555555555555",
			"66666666666666666666666666666666",
			"77777777777777777777777777777777",
			"88888888888888888888888888888888",
		),
	)
	require.NoError(t, err)
	return record
}
