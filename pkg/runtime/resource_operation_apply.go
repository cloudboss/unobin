package runtime

import (
	"context"
	"fmt"
)

type registeredResourceApplyOperationRequest struct {
	Address              string
	Operation            ResourcePlanOperation
	Desired              *PlannedResourceTarget
	DesiredConfiguration any
	DesiredRegistration  *resourceDefinitionRegistration
	Prior                *ResourceTarget
	PriorConfigType      *resolvedConfigurationDefinition
	PriorRegistration    *resourceDefinitionRegistration
	Observation          *ResourceObservation
	DependsOn            []string
	Persist              func(context.Context, *ResourceTarget) error
}

func applyRegisteredResourceOperation(
	ctx context.Context,
	request registeredResourceApplyOperationRequest,
) (*ResourceTarget, error) {
	if ctx == nil {
		return nil, fmt.Errorf("resource apply context is required")
	}
	if err := validateNodeAddress(request.Address, NodeResource); err != nil {
		return nil, err
	}
	if err := request.Operation.Validate(); err != nil {
		return nil, fmt.Errorf("saved resource operation: %w", err)
	}
	if (request.Operation.Desired != nil || request.Desired != nil) &&
		request.DesiredRegistration == nil {
		return nil, fmt.Errorf("desired resource registration is required")
	}
	if (request.Operation.Prior != nil || request.Prior != nil) &&
		request.PriorRegistration == nil {
		return nil, fmt.Errorf("prior resource registration is required")
	}

	prior, priorConfiguration, err := prepareRegisteredResourceApplyPrior(request)
	if err != nil {
		return nil, err
	}
	registration := request.DesiredRegistration
	if request.Operation.Desired == nil {
		registration = request.PriorRegistration
	}
	if registration == nil {
		return nil, fmt.Errorf("resource registration is required")
	}

	callbacks := resourceRegistrationApplyCallbacks{Persist: request.Persist}
	if prior != nil {
		callbacks.ReadPrior = func(ctx context.Context) (ResourceObservation, error) {
			return request.PriorRegistration.readResourceObservation(
				ctx,
				resourceReadRequest{
					Address:       request.Address,
					Binding:       prior.Binding,
					Inputs:        prior.Inputs,
					Configuration: prior.Configuration,
					PriorOutputs:  prior.Outputs,
				},
				priorConfiguration,
			)
		}
		callbacks.DeletePrior = func(ctx context.Context, outputs EncodedValue) error {
			return request.PriorRegistration.deleteResource(
				ctx,
				prior.Inputs,
				priorConfiguration,
				outputs,
			)
		}
	}

	return registration.applyResourceOperation(
		ctx,
		resourceRegistrationApplyRequest{
			Address:              request.Address,
			Operation:            request.Operation,
			Desired:              request.Desired,
			DesiredConfiguration: request.DesiredConfiguration,
			Prior:                prior,
			Observation:          request.Observation,
			DependsOn:            request.DependsOn,
		},
		callbacks,
	)
}

func prepareRegisteredResourceApplyPrior(
	request registeredResourceApplyOperationRequest,
) (*ResourceTarget, any, error) {
	if request.Prior == nil {
		return nil, nil, nil
	}
	prior, configuration, err := prepareRegisteredResourcePriorTarget(
		*request.Prior,
		request.PriorConfigType,
		request.PriorRegistration,
	)
	if err != nil {
		return nil, nil, err
	}
	return &prior, configuration, nil
}
