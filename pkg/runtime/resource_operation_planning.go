package runtime

import (
	"context"
	"fmt"
	"slices"
)

type registeredResourcePlanningRequest struct {
	Address             string
	DependsOn           []string
	Desired             *PlannedResourceTarget
	DesiredConfigType   *resolvedConfigurationDefinition
	DesiredRegistration *resourceDefinitionRegistration
	Prior               *ResourceTarget
	PriorConfigType     *resolvedConfigurationDefinition
	PriorRegistration   *resourceDefinitionRegistration
}

func planRegisteredResourceSteps(
	ctx context.Context,
	evaluate func(*planningPassState) ([]registeredResourcePlanningRequest, error),
) ([]PlanStepV2, error) {
	if evaluate == nil {
		return nil, fmt.Errorf("resource planning evaluator is required")
	}
	return planStepsV2(
		ctx,
		func(pass *planningPassState) ([]planStepV2Request, error) {
			requests, err := evaluate(pass)
			if err != nil {
				return nil, err
			}
			if err := validateRegisteredResourcePlanningRequests(requests); err != nil {
				return nil, err
			}
			planningRequests := make([]planStepV2Request, 0, len(requests))
			for i := range requests {
				request := requests[i]
				if request.Desired == nil && request.Prior == nil {
					continue
				}
				planningRequests = append(planningRequests, planStepV2Request{
					Address:   request.Address,
					Kind:      NodeResource,
					DependsOn: request.DependsOn,
					Plan: func(
						ctx context.Context,
						pass *planningPassState,
					) (*PlanStepV2, error) {
						return planRegisteredResourceStep(ctx, pass, request)
					},
				})
			}
			return planningRequests, nil
		},
	)
}

func validateRegisteredResourcePlanningRequests(
	requests []registeredResourcePlanningRequest,
) error {
	addresses := make(map[string]bool, len(requests))
	for i := range requests {
		request := requests[i]
		if err := validateNodeAddress(request.Address, NodeResource); err != nil {
			return fmt.Errorf("resource request %d: %w", i, err)
		}
		if addresses[request.Address] {
			return fmt.Errorf("duplicate resource address %q", request.Address)
		}
		addresses[request.Address] = true
	}
	for i := range requests {
		request := requests[i]
		if request.Desired == nil && request.Prior == nil {
			continue
		}
		if err := validatePlanDependencies(request.DependsOn); err != nil {
			return fmt.Errorf("%s: %w", request.Address, err)
		}
	}
	return nil
}

func planRegisteredResourceStep(
	ctx context.Context,
	pass *planningPassState,
	request registeredResourcePlanningRequest,
) (*PlanStepV2, error) {
	operation, err := planRegisteredResourceOperation(ctx, pass, request)
	if err != nil {
		return nil, err
	}
	if operation == nil {
		return nil, nil
	}
	step := PlanStepV2{
		Address:   request.Address,
		Kind:      NodeResource,
		DependsOn: slices.Clone(request.DependsOn),
		Operation: StepOperation{
			Kind:     StepResource,
			Resource: operation,
		},
	}
	if err := step.Validate(); err != nil {
		return nil, fmt.Errorf("resource step: %w", err)
	}
	return &step, nil
}

func prepareRegisteredResourceDesiredTarget(
	desired *PlannedResourceTarget,
	configurationDefinition *resolvedConfigurationDefinition,
) (*PlannedResourceTarget, any, error) {
	if desired == nil {
		return nil, nil, nil
	}
	if configurationDefinition == nil {
		return nil, nil, fmt.Errorf(
			"desired configuration definition is required",
		)
	}
	target := clonePlannedResourceTarget(*desired)
	if err := target.Validate(); err != nil {
		return nil, nil, fmt.Errorf("desired target: %w", err)
	}
	if configurationDefinition.libraryPath != target.Binding.LibraryPath {
		return nil, nil, fmt.Errorf(
			"desired configuration definition does not match binding",
		)
	}
	if target.Configuration.Kind == PlannedConfigurationPending {
		return &target, nil, nil
	}
	configuration, decoded, err := configurationDefinition.prepareConfigurationRecord(
		*target.Configuration.Record,
	)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"prepare desired configuration: %w",
			err,
		)
	}
	target.Configuration.Record = &configuration
	if err := target.Validate(); err != nil {
		return nil, nil, fmt.Errorf("prepared desired target: %w", err)
	}
	return &target, decoded, nil
}

func planRegisteredResourceOperation(
	ctx context.Context,
	pass *planningPassState,
	request registeredResourcePlanningRequest,
) (*ResourcePlanOperation, error) {
	if ctx == nil {
		return nil, fmt.Errorf("resource planning context is required")
	}
	if pass == nil {
		return nil, fmt.Errorf("planning pass state is required")
	}
	if err := validateNodeAddress(request.Address, NodeResource); err != nil {
		return nil, err
	}
	if request.Desired != nil && request.DesiredRegistration == nil {
		return nil, fmt.Errorf("desired resource registration is required")
	}
	if request.Prior != nil && request.PriorRegistration == nil {
		return nil, fmt.Errorf("prior resource registration is required")
	}
	if request.Desired == nil && request.Prior == nil {
		return nil, nil
	}

	desired, desiredConfiguration, err := prepareRegisteredResourceDesiredTarget(
		request.Desired,
		request.DesiredConfigType,
	)
	if err != nil {
		return nil, err
	}
	request.Desired = desired
	prior, observation, err := prepareRegisteredResourcePrior(ctx, pass, request)
	if err != nil {
		return nil, err
	}
	desiredObservation, err := readDesiredConfigurationObservation(
		ctx,
		pass,
		request,
		prior,
		observation,
		desiredConfiguration,
	)
	if err != nil {
		return nil, err
	}

	registration := request.DesiredRegistration
	if registration == nil {
		registration = request.PriorRegistration
	}
	operation, err := registration.planResourceOperation(resourcePlanningRequest{
		Desired:                         request.Desired,
		Prior:                           prior,
		RecordedObservation:             observation,
		DesiredConfigurationObservation: desiredObservation,
	})
	if err != nil {
		return nil, err
	}
	return pass.recordResourceOperation(request.Address, operation)
}

func prepareRegisteredResourcePrior(
	ctx context.Context,
	pass *planningPassState,
	request registeredResourcePlanningRequest,
) (*ResourceTarget, *ResourceObservation, error) {
	if request.Prior == nil {
		return nil, nil, nil
	}
	prior, decodedConfiguration, err := prepareRegisteredResourcePriorTarget(
		*request.Prior,
		request.PriorConfigType,
		request.PriorRegistration,
	)
	if err != nil {
		return nil, nil, err
	}
	observation, err := readRegisteredResourceObservation(
		ctx,
		pass,
		request.Address,
		prior.Binding,
		prior.Inputs,
		prior.Configuration,
		prior.Outputs,
		decodedConfiguration,
		request.PriorRegistration,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("read prior resource: %w", err)
	}
	return &prior, &observation, nil
}

func prepareRegisteredResourcePriorTarget(
	prior ResourceTarget,
	configurationDefinition *resolvedConfigurationDefinition,
	registration *resourceDefinitionRegistration,
) (ResourceTarget, any, error) {
	if configurationDefinition == nil {
		return ResourceTarget{}, nil, fmt.Errorf(
			"prior configuration definition is required",
		)
	}
	configuration, decoded, err := configurationDefinition.prepareConfigurationRecord(
		prior.Configuration,
	)
	if err != nil {
		return ResourceTarget{}, nil, fmt.Errorf(
			"prepare prior configuration: %w",
			err,
		)
	}
	prior.Configuration = configuration
	prepared, err := registration.preparePrior(prior)
	if err != nil {
		return ResourceTarget{}, nil, fmt.Errorf("prepare prior resource: %w", err)
	}
	return prepared, decoded, nil
}

func readDesiredConfigurationObservation(
	ctx context.Context,
	pass *planningPassState,
	request registeredResourcePlanningRequest,
	prior *ResourceTarget,
	observation *ResourceObservation,
	desiredConfiguration any,
) (*ResourceObservation, error) {
	if !request.DesiredRegistration.needsDesiredConfigurationRead(
		request.Desired,
		prior,
		observation,
	) {
		return nil, nil
	}
	desired := request.Desired
	result, err := readRegisteredResourceObservation(
		ctx,
		pass,
		request.Address,
		desired.Binding,
		prior.Inputs,
		*desired.Configuration.Record,
		prior.Outputs,
		desiredConfiguration,
		request.DesiredRegistration,
	)
	if err != nil {
		return nil, fmt.Errorf("read resource with desired configuration: %w", err)
	}
	return &result, nil
}

func readRegisteredResourceObservation(
	ctx context.Context,
	pass *planningPassState,
	address string,
	binding Binding,
	inputs EncodedValue,
	configuration ConfigurationRecord,
	priorOutputs EncodedValue,
	decodedConfiguration any,
	registration *resourceDefinitionRegistration,
) (ResourceObservation, error) {
	request := resourceReadRequest{
		Address:       address,
		Binding:       binding,
		Inputs:        inputs,
		Configuration: configuration,
		PriorOutputs:  priorOutputs,
	}
	return pass.readResource(
		ctx,
		request,
		func(ctx context.Context, request resourceReadRequest) (ResourceObservation, error) {
			return registration.readResourceObservation(
				ctx,
				request,
				decodedConfiguration,
			)
		},
	)
}
