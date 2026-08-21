package runtime

import (
	"fmt"
	"slices"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
)

type actionPlanningRequest struct {
	Desired *PlannedActionTarget
	Prior   *ActionStatePayload
}

func planActionOperation(request actionPlanningRequest) (*ActionPlanOperation, error) {
	if request.Desired == nil && request.Prior == nil {
		return nil, nil
	}

	operation := ActionPlanOperation{}
	if request.Desired != nil {
		desired := clonePlannedActionTarget(*request.Desired)
		operation.Desired = &desired
	}
	if request.Prior != nil {
		prior := cloneActionStatePayload(*request.Prior)
		operation.Prior = &prior
	}

	switch {
	case operation.Desired == nil:
		operation.Decision = DecisionDestroy
	case operation.Prior == nil:
		operation.Decision = DecisionRerun
	case operation.Desired.Inputs.HasPending():
		operation.Decision = DecisionRerun
	case operation.Desired.Configuration.Kind != PlannedConfigurationConcrete:
		operation.Decision = DecisionRerun
	case operation.Desired.TriggerHash == "":
		operation.Decision = DecisionRerun
	case operation.Desired.TriggerHash != operation.Prior.TriggerHash:
		operation.Decision = DecisionRerun
	default:
		operation.Decision = DecisionSkip
	}
	if err := operation.Validate(); err != nil {
		return nil, fmt.Errorf("action operation: %w", err)
	}
	return &operation, nil
}

func clonePlannedActionTarget(target PlannedActionTarget) PlannedActionTarget {
	result := target
	result.Configuration.PendingRefs = slices.Clone(target.Configuration.PendingRefs)
	if target.Configuration.Record != nil {
		record := internalconfig.Clone(*target.Configuration.Record)
		result.Configuration.Record = &record
	}
	result.SensitiveInputPaths = slices.Clone(target.SensitiveInputPaths)
	result.SensitiveOutputPaths = slices.Clone(target.SensitiveOutputPaths)
	return result
}

func cloneActionStatePayload(target ActionStatePayload) ActionStatePayload {
	result := target
	result.Configuration = internalconfig.Clone(target.Configuration)
	result.DependsOn = slices.Clone(target.DependsOn)
	result.SensitiveInputPaths = slices.Clone(target.SensitiveInputPaths)
	result.SensitiveOutputPaths = slices.Clone(target.SensitiveOutputPaths)
	return result
}
