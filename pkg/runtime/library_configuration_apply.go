package runtime

import (
	"context"
	"fmt"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
)

type libraryConfigurationApplyRequest struct {
	Address   string
	Operation LibraryConfigurationPlanOperation
	Inputs    EncodedValue
}

type libraryConfigurationApplyCallbacks struct {
	Eval func(context.Context, EncodedValue) (ConfigurationRecord, error)
}

func applyLibraryConfigurationOperation(
	ctx context.Context,
	request libraryConfigurationApplyRequest,
	callbacks libraryConfigurationApplyCallbacks,
) (*ConfigurationRecord, error) {
	if ctx == nil {
		return nil, fmt.Errorf("apply context is required")
	}
	if err := validateNodeAddress(request.Address, NodeLibraryConfiguration); err != nil {
		return nil, err
	}
	if err := request.Operation.Validate(); err != nil {
		return nil, fmt.Errorf("saved library-configuration operation: %w", err)
	}
	if err := validatePlanObject(
		request.Inputs,
		"current library-configuration inputs",
		false,
	); err != nil {
		return nil, err
	}
	if !plannedEncodedValueMatches(request.Operation.Inputs, request.Inputs) {
		return nil, fmt.Errorf("library-configuration inputs do not match the saved plan")
	}
	if callbacks.Eval == nil {
		return nil, fmt.Errorf("library-configuration evaluator is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	result, err := guard(
		"evaluating this library configuration",
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
	if request.Operation.Result.Kind == PlannedConfigurationConcrete &&
		!sameConfigurationRecord(*request.Operation.Result.Record, result) {
		return nil, fmt.Errorf(
			"library-configuration result changed since the plan was computed",
		)
	}

	result = internalconfig.Clone(result)
	return &result, nil
}
