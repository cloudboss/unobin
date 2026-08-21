package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanOutputOperationRecordsEvaluatedValue(t *testing.T) {
	value := operationObject(t, map[string]EncodedValue{
		"endpoint": StringValue("https://example.com"),
	})

	operation, err := planOutputOperation(outputPlanningRequest{
		Value:     value,
		Sensitive: true,
	})
	require.NoError(t, err)
	require.Equal(t, &OutputPlanOperation{
		Decision:  DecisionEval,
		Value:     value,
		Sensitive: true,
	}, operation)
}

func TestPlanOutputOperationAcceptsPendingValue(t *testing.T) {
	pending, err := PendingEncodedValue([]string{"resource.service.url"})
	require.NoError(t, err)
	value := operationObject(t, map[string]EncodedValue{
		"endpoint": pending,
	})

	operation, err := planOutputOperation(outputPlanningRequest{Value: value})
	require.NoError(t, err)
	require.Equal(t, &OutputPlanOperation{
		Decision: DecisionEval,
		Value:    value,
	}, operation)
}

func TestPlanOutputOperationRejectsInvalidValue(t *testing.T) {
	operation, err := planOutputOperation(outputPlanningRequest{})
	require.ErrorContains(t, err, "output value is invalid")
	require.Nil(t, operation)
}
