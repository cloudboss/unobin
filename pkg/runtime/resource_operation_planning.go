package runtime

import (
	"context"
	"fmt"
)

type registeredResourcePlanningRequest struct {
	Address              string
	Desired              *PlannedResourceTarget
	DesiredConfiguration any
	DesiredRegistration  *resourceDefinitionRegistration
	Prior                *ResourceTarget
	PriorConfigType      *resolvedConfigurationDefinition
	PriorRegistration    *resourceDefinitionRegistration
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
		request.DesiredConfiguration,
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
