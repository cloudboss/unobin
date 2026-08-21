package runtime

import "fmt"

type outputPlanningRequest struct {
	Address   string
	DependsOn []string
	Value     EncodedValue
	Sensitive bool
}

func planOutputOperation(request outputPlanningRequest) (*OutputPlanOperation, error) {
	operation := OutputPlanOperation{
		Decision:  DecisionEval,
		Value:     request.Value,
		Sensitive: request.Sensitive,
	}
	if err := operation.Validate(); err != nil {
		return nil, fmt.Errorf("output operation: %w", err)
	}
	return &operation, nil
}
