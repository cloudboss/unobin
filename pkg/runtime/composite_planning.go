package runtime

import (
	"fmt"
	"slices"
)

type compositePlanningRequest struct {
	Address   string
	DependsOn []string
	Category  NodeKind
	Desired   *PlannedCompositeTarget
	Prior     *CompositeStatePayload
}

func planCompositeOperation(
	request compositePlanningRequest,
) (*CompositePlanOperation, error) {
	if request.Desired == nil && request.Prior == nil {
		return nil, nil
	}

	operation := CompositePlanOperation{}
	if request.Desired != nil {
		desired := clonePlannedCompositeTarget(*request.Desired)
		operation.Desired = &desired
	}
	if request.Prior != nil {
		prior := cloneCompositeStatePayload(*request.Prior)
		operation.Prior = &prior
	}

	if operation.Desired == nil {
		operation.Decision = DecisionDestroy
	} else {
		operation.Decision = DecisionEval
	}
	if err := operation.Validate(request.Category); err != nil {
		return nil, fmt.Errorf("composite operation: %w", err)
	}
	return &operation, nil
}

func clonePlannedCompositeTarget(target PlannedCompositeTarget) PlannedCompositeTarget {
	result := target
	result.SensitiveInputPaths = slices.Clone(target.SensitiveInputPaths)
	result.SensitiveOutputPaths = slices.Clone(target.SensitiveOutputPaths)
	return result
}

func cloneCompositeStatePayload(target CompositeStatePayload) CompositeStatePayload {
	result := target
	result.DependsOn = slices.Clone(target.DependsOn)
	result.SensitiveInputPaths = slices.Clone(target.SensitiveInputPaths)
	result.SensitiveOutputPaths = slices.Clone(target.SensitiveOutputPaths)
	return result
}
