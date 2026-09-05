package runtime

import (
	"context"
	"fmt"
)

type registeredResourcePtr[In, Out, Config any] interface {
	*In
	TypedResource[In, Out, Config]
}

type resourceRegistrationApplyRequest struct {
	Address              string
	Operation            ResourcePlanOperation
	Desired              *PlannedResourceTarget
	DesiredConfiguration any
	Prior                *ResourceTarget
	Observation          *ResourceObservation
	DependsOn            []string
}

type resourceRegistrationApplyCallbacks struct {
	ReadPrior   func(context.Context) (ResourceObservation, error)
	DeletePrior func(context.Context, EncodedValue) error
	Persist     func(context.Context, *ResourceTarget) error
}

type resourceDefinitionRegistration struct {
	identityScope     IdentityScope
	prepareInputsFunc func(map[string]any) (EncodedValue, error)
	preparePriorFunc  func(ResourceTarget) (ResourceTarget, error)
	planFunc          func(resourcePlanningRequest) (*ResourcePlanOperation, error)
	readFunc          func(context.Context, resourceReadRequest, any) (ResourceObservation, error)
	applyFunc         func(
		context.Context,
		resourceRegistrationApplyRequest,
		resourceRegistrationApplyCallbacks,
	) (*ResourceTarget, error)
	deleteFunc func(context.Context, EncodedValue, any, EncodedValue) error
}

func newResourceDefinitionRegistration[
	In, Out, Config any,
	PT registeredResourcePtr[In, Out, Config],
](
	definition ResourceDefinition[In, Out, Config],
	construct func() *In,
) (*resourceDefinitionRegistration, error) {
	resolved, err := resolveResourceDefinition(definition)
	if err != nil {
		return nil, fmt.Errorf("resource definition: %w", err)
	}
	return newResolvedResourceDefinitionRegistration[In, Out, Config, PT](resolved, construct), nil
}

func newResolvedResourceDefinitionRegistration[
	In, Out, Config any,
	PT registeredResourcePtr[In, Out, Config],
](
	resolved resolvedResourceDefinition[In, Out, Config],
	construct func() *In,
) *resourceDefinitionRegistration {
	read := newResourceProviderRead[In, Out, Config, PT](construct)
	create := newResourceProviderCreate[In, Out, Config, PT](construct)
	update := newResourceProviderUpdate[In, Out, Config, PT](construct)
	deleteResource := newResourceProviderDelete[In, Out, Config, PT](construct)

	return &resourceDefinitionRegistration{
		identityScope: resolved.identityScope,
		prepareInputsFunc: func(values map[string]any) (EncodedValue, error) {
			encoded, _, err := prepareResourceInputs[In](values)
			return encoded, err
		},
		preparePriorFunc: func(target ResourceTarget) (ResourceTarget, error) {
			prepared, err := resolved.prepareResourcePrior(&target)
			if err != nil {
				return ResourceTarget{}, err
			}
			return prepared.Target, nil
		},
		planFunc: resolved.planResourceOperation,
		readFunc: func(
			ctx context.Context,
			request resourceReadRequest,
			configuration any,
		) (ResourceObservation, error) {
			config, err := coerceConfig[Config](configuration)
			if err != nil {
				return ResourceObservation{}, fmt.Errorf("resource configuration: %w", err)
			}
			return resolved.readResourceObservation(ctx, request, config, read)
		},
		applyFunc: func(
			ctx context.Context,
			request resourceRegistrationApplyRequest,
			callbacks resourceRegistrationApplyCallbacks,
		) (*ResourceTarget, error) {
			prepared, err := resolved.prepareResourcePlanningRequest(resourcePlanningRequest{
				Desired:             request.Desired,
				Prior:               request.Prior,
				RecordedObservation: request.Observation,
			})
			if err != nil {
				return nil, err
			}
			var config Config
			if request.Desired != nil {
				config, err = coerceConfig[Config](request.DesiredConfiguration)
				if err != nil {
					return nil, fmt.Errorf("resource configuration: %w", err)
				}
			}
			return resolved.applyResourceOperation(
				ctx,
				resourceApplyRequest[In, Out, Config]{
					Address:              request.Address,
					Operation:            request.Operation,
					Desired:              prepared.Desired,
					DesiredConfiguration: config,
					Prior:                prepared.Prior,
					Observation:          prepared.RecordedObservation,
					DependsOn:            request.DependsOn,
				},
				resourceApplyCallbacks[In, Out, Config]{
					ReadDesired: func(
						ctx context.Context,
						inputs In,
						configuration Config,
						prior Out,
					) (preparedResourceApplyResult[Out], error) {
						outputs, err := read(ctx, inputs, configuration, prior)
						if err != nil {
							return preparedResourceApplyResult[Out]{}, err
						}
						return prepareResourceProviderResult(outputs)
					},
					ReadPrior:   callbacks.ReadPrior,
					Create:      create,
					Update:      update,
					DeletePrior: callbacks.DeletePrior,
					Persist:     callbacks.Persist,
				},
			)
		},
		deleteFunc: func(
			ctx context.Context,
			inputs EncodedValue,
			configuration any,
			outputs EncodedValue,
		) error {
			decodedInputs, err := decodeResourceInputs[In](inputs)
			if err != nil {
				return fmt.Errorf("delete resource inputs: %w", err)
			}
			config, err := coerceConfig[Config](configuration)
			if err != nil {
				return fmt.Errorf("resource configuration: %w", err)
			}
			return deleteResource(ctx, decodedInputs, config, outputs)
		},
	}
}

func (r *resourceDefinitionRegistration) needsDesiredConfigurationRead(
	desired *PlannedResourceTarget,
	prior *ResourceTarget,
	observation *ResourceObservation,
) bool {
	if r == nil || r.identityScope != IdentityGlobal ||
		desired == nil || prior == nil || observation == nil {
		return false
	}
	if observation.Status != ObservationPresent || desired.Binding != prior.Binding {
		return false
	}
	if desired.Configuration.Kind != PlannedConfigurationConcrete ||
		desired.Configuration.Record == nil {
		return false
	}
	return desired.Configuration.Record.Digest != prior.Configuration.Digest
}

func (r *resourceDefinitionRegistration) prepareInputs(
	values map[string]any,
) (EncodedValue, error) {
	return r.prepareInputsFunc(values)
}

func (r *resourceDefinitionRegistration) preparePrior(
	target ResourceTarget,
) (ResourceTarget, error) {
	return r.preparePriorFunc(target)
}

func (r *resourceDefinitionRegistration) planResourceOperation(
	request resourcePlanningRequest,
) (*ResourcePlanOperation, error) {
	return r.planFunc(request)
}

func (r *resourceDefinitionRegistration) readResourceObservation(
	ctx context.Context,
	request resourceReadRequest,
	configuration any,
) (ResourceObservation, error) {
	return r.readFunc(ctx, request, configuration)
}

func (r *resourceDefinitionRegistration) applyResourceOperation(
	ctx context.Context,
	request resourceRegistrationApplyRequest,
	callbacks resourceRegistrationApplyCallbacks,
) (*ResourceTarget, error) {
	return r.applyFunc(ctx, request, callbacks)
}

func (r *resourceDefinitionRegistration) deleteResource(
	ctx context.Context,
	inputs EncodedValue,
	configuration any,
	outputs EncodedValue,
) error {
	return r.deleteFunc(ctx, inputs, configuration, outputs)
}
