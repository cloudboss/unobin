package runtime

import (
	"context"
	"fmt"
	"slices"
)

func planActionStep(request actionPlanningRequest) (*PlanStepV2, error) {
	if request.Desired == nil && request.Prior == nil {
		return nil, nil
	}
	if err := validatePlanningStepMetadata(
		request.Address,
		NodeAction,
		request.DependsOn,
	); err != nil {
		return nil, fmt.Errorf("action step: %w", err)
	}
	operation, err := planActionOperation(request)
	if err != nil {
		return nil, err
	}
	return newPlanningStep(
		"action",
		request.Address,
		NodeAction,
		request.DependsOn,
		StepOperation{Kind: StepAction, Action: operation},
	)
}

func planDataSourceStep(
	ctx context.Context,
	request dataSourcePlanningRequest,
	callbacks dataSourcePlanningCallbacks,
) (*PlanStepV2, error) {
	if ctx == nil {
		return nil, fmt.Errorf("data-source planning context is required")
	}
	if request.Desired == nil && request.Prior == nil {
		return nil, nil
	}
	if err := validatePlanningStepMetadata(
		request.Address,
		NodeDataSource,
		request.DependsOn,
	); err != nil {
		return nil, fmt.Errorf("data-source step: %w", err)
	}
	operation, err := planDataSourceOperation(ctx, request, callbacks)
	if err != nil {
		return nil, err
	}
	return newPlanningStep(
		"data-source",
		request.Address,
		NodeDataSource,
		request.DependsOn,
		StepOperation{Kind: StepDataSource, DataSource: operation},
	)
}

func planLibraryConfigurationStep(
	ctx context.Context,
	request libraryConfigurationPlanningRequest,
	callbacks libraryConfigurationPlanningCallbacks,
) (*PlanStepV2, error) {
	if ctx == nil {
		return nil, fmt.Errorf("library-configuration planning context is required")
	}
	if err := validatePlanningStepMetadata(
		request.Address,
		NodeLibraryConfiguration,
		request.DependsOn,
	); err != nil {
		return nil, fmt.Errorf("library-configuration step: %w", err)
	}
	operation, err := planLibraryConfigurationOperation(ctx, request, callbacks)
	if err != nil {
		return nil, err
	}
	return newPlanningStep(
		"library-configuration",
		request.Address,
		NodeLibraryConfiguration,
		request.DependsOn,
		StepOperation{
			Kind:                 StepLibraryConfiguration,
			LibraryConfiguration: operation,
		},
	)
}

func planCompositeStep(request compositePlanningRequest) (*PlanStepV2, error) {
	if request.Desired == nil && request.Prior == nil {
		return nil, nil
	}
	if err := validatePlanningStepMetadata(
		request.Address,
		request.Category,
		request.DependsOn,
	); err != nil {
		return nil, fmt.Errorf("composite step: %w", err)
	}
	operation, err := planCompositeOperation(request)
	if err != nil {
		return nil, err
	}
	return newPlanningStep(
		"composite",
		request.Address,
		request.Category,
		request.DependsOn,
		StepOperation{Kind: StepComposite, Composite: operation},
	)
}

func planOutputStep(request outputPlanningRequest) (*PlanStepV2, error) {
	if err := validatePlanningStepMetadata(
		request.Address,
		NodeOutput,
		request.DependsOn,
	); err != nil {
		return nil, fmt.Errorf("output step: %w", err)
	}
	operation, err := planOutputOperation(request)
	if err != nil {
		return nil, err
	}
	return newPlanningStep(
		"output",
		request.Address,
		NodeOutput,
		request.DependsOn,
		StepOperation{Kind: StepOutput, Output: operation},
	)
}

func validatePlanningStepMetadata(
	address string,
	kind NodeKind,
	dependencies []string,
) error {
	if err := validateNodeAddress(address, kind); err != nil {
		return err
	}
	return validatePlanDependencies(dependencies)
}

func newPlanningStep(
	subject string,
	address string,
	kind NodeKind,
	dependencies []string,
	operation StepOperation,
) (*PlanStepV2, error) {
	step := PlanStepV2{
		Address:   address,
		Kind:      kind,
		DependsOn: slices.Clone(dependencies),
		Operation: operation,
	}
	if err := step.Validate(); err != nil {
		return nil, fmt.Errorf("%s step: %w", subject, err)
	}
	return &step, nil
}
