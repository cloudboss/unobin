package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyCompositeOperationEvaluatesResolvedTarget(t *testing.T) {
	planned := validPlannedCompositeTarget(t, NodeResource)
	current := planned
	current.Inputs = operationObject(t, map[string]EncodedValue{
		"name":  StringValue("api"),
		"token": StringValue("secret"),
	})
	planned.Inputs = current.Inputs
	planned.SensitiveInputPaths = []string{"/token"}
	current.SensitiveInputPaths = []string{"/token"}
	planned.SensitiveOutputPaths = []string{"/secret"}
	current.SensitiveOutputPaths = []string{"/secret"}
	outputs := operationObject(t, map[string]EncodedValue{
		"secret": StringValue("result"),
		"url":    StringValue("https://example.com"),
	})
	request := compositeApplyRequest{
		Address:  "resource.application",
		Category: NodeResource,
		Operation: CompositePlanOperation{
			Decision: DecisionEval,
			Desired:  &planned,
		},
		Desired:   &current,
		DependsOn: []string{"data-source.config", "resource.network"},
	}
	var events []string
	var persisted *CompositeStatePayload

	target, err := applyCompositeOperation(
		context.Background(),
		request,
		compositeApplyCallbacks{
			Eval: func(context.Context) (EncodedValue, error) {
				events = append(events, "eval")
				return outputs, nil
			},
			Persist: func(_ context.Context, target *CompositeStatePayload) error {
				events = append(events, "persist")
				persisted = target
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"eval", "persist"}, events)
	expected := CompositeStatePayload{
		Category:             string(NodeResource),
		Binding:              current.Binding,
		Inputs:               current.Inputs,
		Outputs:              outputs,
		DependsOn:            []string{"data-source.config", "resource.network"},
		SensitiveInputPaths:  []string{"/token"},
		SensitiveOutputPaths: []string{"/secret"},
	}
	require.Equal(t, &expected, target)
	require.Equal(t, target, persisted)

	request.DependsOn[0] = "action.changed"
	current.SensitiveInputPaths[0] = "/name"
	current.SensitiveOutputPaths[0] = "/url"
	require.Equal(t, []string{"data-source.config", "resource.network"}, target.DependsOn)
	require.Equal(t, []string{"/token"}, target.SensitiveInputPaths)
	require.Equal(t, []string{"/secret"}, target.SensitiveOutputPaths)
}

func TestApplyCompositeOperationAcceptsResolvedPendingInputs(t *testing.T) {
	pending, err := PendingEncodedValue([]string{"resource.network.id"})
	require.NoError(t, err)
	planned := validPlannedCompositeTarget(t, NodeResource)
	planned.Inputs = operationObject(t, map[string]EncodedValue{"name": pending})
	current := validPlannedCompositeTarget(t, NodeResource)
	outputs := operationObject(t, map[string]EncodedValue{
		"url": StringValue("https://example.com"),
	})

	target, err := applyCompositeOperation(
		context.Background(),
		compositeApplyRequest{
			Address:  "resource.application",
			Category: NodeResource,
			Operation: CompositePlanOperation{
				Decision: DecisionEval,
				Desired:  &planned,
			},
			Desired:   &current,
			DependsOn: []string{},
		},
		compositeApplyCallbacks{
			Eval: func(context.Context) (EncodedValue, error) {
				return outputs, nil
			},
			Persist: func(context.Context, *CompositeStatePayload) error {
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, current.Inputs, target.Inputs)
	require.Equal(t, outputs, target.Outputs)
}

func TestApplyCompositeOperationDestroysStateWithoutEvaluation(t *testing.T) {
	prior := operationCompositeState(t, NodeAction)
	evaluated := false
	persisted := false

	target, err := applyCompositeOperation(
		context.Background(),
		compositeApplyRequest{
			Address:  "action.release",
			Category: NodeAction,
			Operation: CompositePlanOperation{
				Decision: DecisionDestroy,
				Prior:    &prior,
			},
			Prior:     &prior,
			DependsOn: []string{},
		},
		compositeApplyCallbacks{
			Eval: func(context.Context) (EncodedValue, error) {
				evaluated = true
				return EncodedValue{}, nil
			},
			Persist: func(_ context.Context, target *CompositeStatePayload) error {
				persisted = true
				require.Nil(t, target)
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Nil(t, target)
	require.False(t, evaluated)
	require.True(t, persisted)
}

func TestApplyCompositeOperationRejectsChangedPremises(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*compositeApplyRequest, *compositeApplyCallbacks)
		message string
	}{
		{
			name: "missing desired target",
			change: func(request *compositeApplyRequest, _ *compositeApplyCallbacks) {
				request.Desired = nil
			},
			message: "saved operation requires a desired composite",
		},
		{
			name: "known input changed",
			change: func(request *compositeApplyRequest, _ *compositeApplyCallbacks) {
				request.Desired.Inputs = operationObject(t, map[string]EncodedValue{
					"name": StringValue("changed"),
				})
			},
			message: "desired inputs do not match the saved plan",
		},
		{
			name: "category changed",
			change: func(request *compositeApplyRequest, _ *compositeApplyCallbacks) {
				request.Desired.Category = NodeAction
			},
			message: "desired category does not match the saved plan",
		},
		{
			name: "binding changed",
			change: func(request *compositeApplyRequest, _ *compositeApplyCallbacks) {
				request.Desired.Binding.Export = "service"
			},
			message: "desired binding does not match the saved plan",
		},
		{
			name: "current input remains pending",
			change: func(request *compositeApplyRequest, _ *compositeApplyCallbacks) {
				pending, err := PendingEncodedValue([]string{"resource.network.id"})
				require.NoError(t, err)
				request.Desired.Inputs = operationObject(t, map[string]EncodedValue{
					"name": pending,
				})
			},
			message: "desired inputs did not resolve before apply",
		},
		{
			name: "sensitive paths changed",
			change: func(request *compositeApplyRequest, _ *compositeApplyCallbacks) {
				request.Desired.SensitiveInputPaths = []string{"/name"}
			},
			message: "sensitive input paths do not match the saved plan",
		},
		{
			name: "unexpected prior state",
			change: func(request *compositeApplyRequest, _ *compositeApplyCallbacks) {
				prior := operationCompositeState(t, NodeResource)
				request.Prior = &prior
			},
			message: "saved operation forbids prior composite state",
		},
		{
			name: "missing prior state",
			change: func(request *compositeApplyRequest, _ *compositeApplyCallbacks) {
				prior := operationCompositeState(t, NodeResource)
				request.Operation.Prior = &prior
			},
			message: "saved operation requires prior composite state",
		},
		{
			name: "changed prior state",
			change: func(request *compositeApplyRequest, _ *compositeApplyCallbacks) {
				planned := operationCompositeState(t, NodeResource)
				current := planned
				current.Outputs = operationObject(t, map[string]EncodedValue{
					"url": StringValue("https://changed.example.com"),
				})
				request.Operation.Prior = &planned
				request.Prior = &current
			},
			message: "prior composite state does not match the saved plan",
		},
		{
			name: "destroy no longer absent",
			change: func(request *compositeApplyRequest, _ *compositeApplyCallbacks) {
				prior := operationCompositeState(t, NodeResource)
				request.Operation = CompositePlanOperation{
					Decision: DecisionDestroy,
					Prior:    &prior,
				}
				request.Prior = &prior
			},
			message: "saved destroy requires the composite to remain absent",
		},
		{
			name: "missing dependencies",
			change: func(request *compositeApplyRequest, _ *compositeApplyCallbacks) {
				request.DependsOn = nil
			},
			message: "dependencies are required",
		},
		{
			name: "missing eval callback",
			change: func(_ *compositeApplyRequest, callbacks *compositeApplyCallbacks) {
				callbacks.Eval = nil
			},
			message: "composite eval callback is required",
		},
		{
			name: "missing persistence callback",
			change: func(_ *compositeApplyRequest, callbacks *compositeApplyCallbacks) {
				callbacks.Persist = nil
			},
			message: "composite state persistence callback is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plannedDesired := validPlannedCompositeTarget(t, NodeResource)
			currentDesired := plannedDesired
			request := compositeApplyRequest{
				Address:  "resource.application",
				Category: NodeResource,
				Operation: CompositePlanOperation{
					Decision: DecisionEval,
					Desired:  &plannedDesired,
				},
				Desired:   &currentDesired,
				DependsOn: []string{},
			}
			evaluated := false
			persisted := false
			callbacks := compositeApplyCallbacks{
				Eval: func(context.Context) (EncodedValue, error) {
					evaluated = true
					return operationObject(t, map[string]EncodedValue{}), nil
				},
				Persist: func(context.Context, *CompositeStatePayload) error {
					persisted = true
					return nil
				},
			}
			test.change(&request, &callbacks)

			target, err := applyCompositeOperation(
				context.Background(),
				request,
				callbacks,
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
			require.False(t, evaluated)
			require.False(t, persisted)
		})
	}
}

func TestApplyCompositeOperationRejectsInvalidSetup(t *testing.T) {
	desired := validPlannedCompositeTarget(t, NodeResource)
	request := compositeApplyRequest{
		Address:  "resource.application",
		Category: NodeResource,
		Operation: CompositePlanOperation{
			Decision: DecisionEval,
			Desired:  &desired,
		},
		Desired:   &desired,
		DependsOn: []string{},
	}
	callbacks := compositeApplyCallbacks{
		Eval: func(context.Context) (EncodedValue, error) {
			return operationObject(t, map[string]EncodedValue{}), nil
		},
		Persist: func(context.Context, *CompositeStatePayload) error { return nil },
	}
	var nilContext context.Context

	target, err := applyCompositeOperation(nilContext, request, callbacks)
	require.ErrorContains(t, err, "apply context is required")
	require.Nil(t, target)

	request.Address = "action.application"
	target, err = applyCompositeOperation(context.Background(), request, callbacks)
	require.ErrorContains(t, err, "address category action does not match resource")
	require.Nil(t, target)

	request.Address = "resource.application"
	request.Category = NodeKind("unknown")
	target, err = applyCompositeOperation(context.Background(), request, callbacks)
	require.ErrorContains(t, err, "node kind is invalid")
	require.Nil(t, target)
}

func TestApplyCompositeOperationReportsExecutionFailures(t *testing.T) {
	expectedErr := errors.New("operation failed")
	tests := []struct {
		name       string
		eval       func(context.Context) (EncodedValue, error)
		persistErr error
		message    string
	}{
		{
			name: "eval error",
			eval: func(context.Context) (EncodedValue, error) {
				return EncodedValue{}, expectedErr
			},
			message: "evaluate composite",
		},
		{
			name: "eval panic",
			eval: func(context.Context) (EncodedValue, error) {
				panic("failed")
			},
			message: "panic in the library while evaluating this composite",
		},
		{
			name: "non-object outputs",
			eval: func(context.Context) (EncodedValue, error) {
				return StringValue("bad"), nil
			},
			message: "composite outputs must be an object",
		},
		{
			name: "persistence error",
			eval: func(context.Context) (EncodedValue, error) {
				return operationObject(t, map[string]EncodedValue{}), nil
			},
			persistErr: expectedErr,
			message:    "persist composite state",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			desired := validPlannedCompositeTarget(t, NodeResource)
			request := compositeApplyRequest{
				Address:  "resource.application",
				Category: NodeResource,
				Operation: CompositePlanOperation{
					Decision: DecisionEval,
					Desired:  &desired,
				},
				Desired:   &desired,
				DependsOn: []string{},
			}
			persisted := false

			target, err := applyCompositeOperation(
				context.Background(),
				request,
				compositeApplyCallbacks{
					Eval: test.eval,
					Persist: func(context.Context, *CompositeStatePayload) error {
						persisted = true
						return test.persistErr
					},
				},
			)
			require.ErrorContains(t, err, test.message)
			if test.message == "evaluate composite" || test.persistErr != nil {
				require.ErrorIs(t, err, expectedErr)
			}
			require.Nil(t, target)
			require.Equal(t, test.persistErr != nil, persisted)
		})
	}
}

func validPlannedCompositeTarget(t *testing.T, category NodeKind) PlannedCompositeTarget {
	t.Helper()
	return PlannedCompositeTarget{
		Category: category,
		Binding: Binding{
			LibraryPath: "example.com/composites",
			Export:      "application",
		},
		Inputs: operationObject(t, map[string]EncodedValue{
			"name": StringValue("api"),
		}),
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
}
