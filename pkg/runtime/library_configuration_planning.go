package runtime

import (
	"context"
	"fmt"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
)

type libraryConfigurationPlanningRequest struct {
	Address   string
	DependsOn []string
	Inputs    EncodedValue
}

type libraryConfigurationPlanningCallbacks struct {
	Eval func(context.Context, EncodedValue) (ConfigurationRecord, error)
}

func planLibraryConfigurationOperation(
	ctx context.Context,
	request libraryConfigurationPlanningRequest,
	callbacks libraryConfigurationPlanningCallbacks,
) (*LibraryConfigurationPlanOperation, error) {
	if ctx == nil {
		return nil, fmt.Errorf("library-configuration planning context is required")
	}
	if err := validatePlanObject(
		request.Inputs,
		"library-configuration inputs",
		true,
	); err != nil {
		return nil, err
	}

	operation := LibraryConfigurationPlanOperation{
		Decision: DecisionEval,
		Inputs:   request.Inputs,
	}
	refs := pendingReferences(request.Inputs)
	if len(refs) > 0 {
		operation.Result = PlannedConfiguration{
			Kind:        PlannedConfigurationPending,
			PendingRefs: refs,
		}
	} else {
		if callbacks.Eval == nil {
			return nil, fmt.Errorf("library-configuration evaluator is required")
		}
		result, err := guard(
			"evaluating this library configuration during planning",
			false,
			func() (ConfigurationRecord, error) {
				return callbacks.Eval(ctx, request.Inputs)
			},
		)
		if err != nil {
			return nil, fmt.Errorf("evaluate library configuration: %w", err)
		}
		if err := result.Validate(); err != nil {
			return nil, fmt.Errorf("evaluated library configuration: %w", err)
		}
		result = internalconfig.Clone(result)
		operation.Result = PlannedConfiguration{
			Kind:   PlannedConfigurationConcrete,
			Record: &result,
		}
	}
	if err := operation.Validate(); err != nil {
		return nil, fmt.Errorf("library-configuration operation: %w", err)
	}
	return &operation, nil
}
