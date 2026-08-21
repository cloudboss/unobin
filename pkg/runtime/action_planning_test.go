package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanActionOperationClassifiesTargetPresence(t *testing.T) {
	desired := validPlannedActionTarget(t)
	prior := operationActionState(t)

	tests := []struct {
		name     string
		request  actionPlanningRequest
		expected *ActionPlanOperation
	}{
		{
			name:    "absent",
			request: actionPlanningRequest{},
		},
		{
			name: "initial rerun",
			request: actionPlanningRequest{
				Desired: &desired,
			},
			expected: &ActionPlanOperation{
				Decision: DecisionRerun,
				Desired:  &desired,
			},
		},
		{
			name: "destroy",
			request: actionPlanningRequest{
				Prior: &prior,
			},
			expected: &ActionPlanOperation{
				Decision: DecisionDestroy,
				Prior:    &prior,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation, err := planActionOperation(test.request)
			require.NoError(t, err)
			require.Equal(t, test.expected, operation)
		})
	}
}

func TestPlanActionOperationClassifiesExistingTarget(t *testing.T) {
	prior := operationActionState(t)

	tests := []struct {
		name     string
		desired  func() PlannedActionTarget
		decision Decision
	}{
		{
			name: "equal concrete trigger skips",
			desired: func() PlannedActionTarget {
				return validPlannedActionTarget(t)
			},
			decision: DecisionSkip,
		},
		{
			name: "changed trigger reruns",
			desired: func() PlannedActionTarget {
				desired := validPlannedActionTarget(t)
				desired.TriggerHash = "trigger-2"
				return desired
			},
			decision: DecisionRerun,
		},
		{
			name: "empty trigger reruns",
			desired: func() PlannedActionTarget {
				desired := validPlannedActionTarget(t)
				desired.TriggerHash = ""
				return desired
			},
			decision: DecisionRerun,
		},
		{
			name: "pending inputs rerun",
			desired: func() PlannedActionTarget {
				desired := validPlannedActionTarget(t)
				pending, err := PendingEncodedValue([]string{"resource.service.id"})
				require.NoError(t, err)
				desired.Inputs = operationObject(t, map[string]EncodedValue{
					"message": pending,
				})
				return desired
			},
			decision: DecisionRerun,
		},
		{
			name: "pending configuration reruns",
			desired: func() PlannedActionTarget {
				desired := validPlannedActionTarget(t)
				desired.Configuration = pendingOperationConfiguration()
				return desired
			},
			decision: DecisionRerun,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			desired := test.desired()
			operation, err := planActionOperation(actionPlanningRequest{
				Desired: &desired,
				Prior:   &prior,
			})
			require.NoError(t, err)
			require.Equal(t, &ActionPlanOperation{
				Decision: test.decision,
				Desired:  &desired,
				Prior:    &prior,
			}, operation)
		})
	}
}

func TestPlanActionOperationRejectsInvalidTargets(t *testing.T) {
	tests := []struct {
		name    string
		request func() actionPlanningRequest
		message string
	}{
		{
			name: "desired",
			request: func() actionPlanningRequest {
				desired := validPlannedActionTarget(t)
				desired.Binding = Binding{}
				return actionPlanningRequest{Desired: &desired}
			},
			message: "desired target: binding: library path is required",
		},
		{
			name: "prior",
			request: func() actionPlanningRequest {
				prior := operationActionState(t)
				prior.Outputs = StringValue("invalid")
				return actionPlanningRequest{Prior: &prior}
			},
			message: "prior state: outputs must be an object",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation, err := planActionOperation(test.request())
			require.ErrorContains(t, err, test.message)
			require.Nil(t, operation)
		})
	}
}

func TestPlanActionOperationCopiesTargets(t *testing.T) {
	desired := validPlannedActionTarget(t)
	desired.SensitiveInputPaths = []string{"/message"}
	desired.SensitiveOutputPaths = []string{"/sent"}
	prior := operationActionState(t)
	prior.SensitiveInputPaths = []string{"/message"}
	prior.SensitiveOutputPaths = []string{"/sent"}

	operation, err := planActionOperation(actionPlanningRequest{
		Desired: &desired,
		Prior:   &prior,
	})
	require.NoError(t, err)
	require.NotSame(t, &desired, operation.Desired)
	require.NotSame(t, &prior, operation.Prior)
	require.NotSame(t, desired.Configuration.Record, operation.Desired.Configuration.Record)

	desired.SensitiveInputPaths[0] = "/changed"
	desired.Configuration.Record.SensitivePaths[0] = "/changed"
	prior.DependsOn = append(prior.DependsOn, "resource.changed")
	prior.SensitiveOutputPaths[0] = "/changed"
	prior.Configuration.SensitivePaths[0] = "/changed"

	require.Equal(t, []string{"/message"}, operation.Desired.SensitiveInputPaths)
	require.NotEqual(t, "/changed", operation.Desired.Configuration.Record.SensitivePaths[0])
	require.Empty(t, operation.Prior.DependsOn)
	require.Equal(t, []string{"/sent"}, operation.Prior.SensitiveOutputPaths)
	require.NotEqual(t, "/changed", operation.Prior.Configuration.SensitivePaths[0])

	pendingDesired := validPlannedActionTarget(t)
	pendingDesired.Configuration = pendingOperationConfiguration()
	pendingOperation, err := planActionOperation(actionPlanningRequest{
		Desired: &pendingDesired,
	})
	require.NoError(t, err)
	pendingDesired.Configuration.PendingRefs[0] = "resource.changed.id"
	require.Equal(
		t,
		[]string{"resource.network.id"},
		pendingOperation.Desired.Configuration.PendingRefs,
	)
}
