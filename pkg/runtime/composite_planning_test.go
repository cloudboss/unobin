package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanCompositeOperationClassifiesTargetPresence(t *testing.T) {
	desired := validPlannedCompositeTarget(t, NodeResource)
	prior := operationCompositeState(t, NodeResource)

	tests := []struct {
		name     string
		request  compositePlanningRequest
		expected *CompositePlanOperation
	}{
		{
			name: "absent",
		},
		{
			name: "initial eval",
			request: compositePlanningRequest{
				Category: NodeResource,
				Desired:  &desired,
			},
			expected: &CompositePlanOperation{
				Decision: DecisionEval,
				Desired:  &desired,
			},
		},
		{
			name: "existing eval",
			request: compositePlanningRequest{
				Category: NodeResource,
				Desired:  &desired,
				Prior:    &prior,
			},
			expected: &CompositePlanOperation{
				Decision: DecisionEval,
				Desired:  &desired,
				Prior:    &prior,
			},
		},
		{
			name: "destroy",
			request: compositePlanningRequest{
				Category: NodeResource,
				Prior:    &prior,
			},
			expected: &CompositePlanOperation{
				Decision: DecisionDestroy,
				Prior:    &prior,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation, err := planCompositeOperation(test.request)
			require.NoError(t, err)
			require.Equal(t, test.expected, operation)
		})
	}
}

func TestPlanCompositeOperationAcceptsCategoriesAndPendingInputs(t *testing.T) {
	pending, err := PendingEncodedValue([]string{"resource.network.id"})
	require.NoError(t, err)

	for _, category := range []NodeKind{NodeResource, NodeAction, NodeDataSource} {
		t.Run(string(category), func(t *testing.T) {
			desired := validPlannedCompositeTarget(t, category)
			desired.Inputs = operationObject(t, map[string]EncodedValue{
				"name": pending,
			})

			operation, err := planCompositeOperation(compositePlanningRequest{
				Category: category,
				Desired:  &desired,
			})
			require.NoError(t, err)
			require.Equal(t, &CompositePlanOperation{
				Decision: DecisionEval,
				Desired:  &desired,
			}, operation)
		})
	}
}

func TestPlanCompositeOperationRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		request func() compositePlanningRequest
		message string
	}{
		{
			name: "step category",
			request: func() compositePlanningRequest {
				desired := validPlannedCompositeTarget(t, NodeResource)
				return compositePlanningRequest{Category: NodeOutput, Desired: &desired}
			},
			message: `composite step category is invalid: "output"`,
		},
		{
			name: "desired category",
			request: func() compositePlanningRequest {
				desired := validPlannedCompositeTarget(t, NodeAction)
				return compositePlanningRequest{Category: NodeResource, Desired: &desired}
			},
			message: "desired category action does not match resource",
		},
		{
			name: "desired target",
			request: func() compositePlanningRequest {
				desired := validPlannedCompositeTarget(t, NodeResource)
				desired.Binding = Binding{}
				return compositePlanningRequest{Category: NodeResource, Desired: &desired}
			},
			message: "desired target: binding: library path is required",
		},
		{
			name: "prior category",
			request: func() compositePlanningRequest {
				prior := operationCompositeState(t, NodeAction)
				return compositePlanningRequest{Category: NodeResource, Prior: &prior}
			},
			message: "prior category action does not match resource",
		},
		{
			name: "prior state",
			request: func() compositePlanningRequest {
				prior := operationCompositeState(t, NodeResource)
				prior.Outputs = StringValue("invalid")
				return compositePlanningRequest{Category: NodeResource, Prior: &prior}
			},
			message: "prior state: outputs must be an object",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation, err := planCompositeOperation(test.request())
			require.ErrorContains(t, err, test.message)
			require.Nil(t, operation)
		})
	}
}

func TestPlanCompositeOperationCopiesTargets(t *testing.T) {
	desired := validPlannedCompositeTarget(t, NodeResource)
	desired.SensitiveInputPaths = []string{"/name"}
	desired.SensitiveOutputPaths = []string{"/url"}
	prior := operationCompositeState(t, NodeResource)
	prior.DependsOn = []string{"resource.network"}
	prior.SensitiveInputPaths = []string{"/name"}
	prior.SensitiveOutputPaths = []string{"/url"}

	operation, err := planCompositeOperation(compositePlanningRequest{
		Category: NodeResource,
		Desired:  &desired,
		Prior:    &prior,
	})
	require.NoError(t, err)
	require.NotSame(t, &desired, operation.Desired)
	require.NotSame(t, &prior, operation.Prior)

	desired.SensitiveInputPaths[0] = "/changed"
	desired.SensitiveOutputPaths[0] = "/changed"
	prior.DependsOn[0] = "resource.changed"
	prior.SensitiveInputPaths[0] = "/changed"
	prior.SensitiveOutputPaths[0] = "/changed"

	require.Equal(t, []string{"/name"}, operation.Desired.SensitiveInputPaths)
	require.Equal(t, []string{"/url"}, operation.Desired.SensitiveOutputPaths)
	require.Equal(t, []string{"resource.network"}, operation.Prior.DependsOn)
	require.Equal(t, []string{"/name"}, operation.Prior.SensitiveInputPaths)
	require.Equal(t, []string{"/url"}, operation.Prior.SensitiveOutputPaths)
}
