package runtime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
)

type preparedResourceApplyResult[Out any] struct {
	Outputs Out
	Encoded EncodedValue
}

type resourceApplyRequest[In, Out, Config any] struct {
	Address              string
	Operation            ResourcePlanOperation
	Desired              *preparedResourceDesired[In]
	DesiredConfiguration Config
	Prior                *preparedResourcePrior[In, Out]
	Observation          *preparedResourceObservation[Out]
	DependsOn            []string
}

type resourceApplyCallbacks[In, Out, Config any] struct {
	ReadDesired func(
		context.Context,
		In,
		Config,
		Out,
	) (preparedResourceApplyResult[Out], error)
	ReadPrior func(context.Context) (ResourceObservation, error)
	Create    func(
		context.Context,
		In,
		Config,
	) (preparedResourceApplyResult[Out], error)
	Update func(
		context.Context,
		In,
		Config,
		Prior[In, Out],
	) (preparedResourceApplyResult[Out], error)
	DeletePrior func(context.Context, EncodedValue) error
	Persist     func(context.Context, *ResourceTarget) error
}

func (d resolvedResourceDefinition[In, Out, Config]) applyResourceOperation(
	ctx context.Context,
	request resourceApplyRequest[In, Out, Config],
	callbacks resourceApplyCallbacks[In, Out, Config],
) (*ResourceTarget, error) {
	if ctx == nil {
		return nil, fmt.Errorf("apply context is required")
	}
	if err := validateNodeAddress(request.Address, NodeResource); err != nil {
		return nil, err
	}
	if err := request.Operation.Validate(); err != nil {
		return nil, fmt.Errorf("saved resource operation: %w", err)
	}
	if err := validateResourceApplyPremises(request); err != nil {
		return nil, err
	}
	if err := validateResourceApplyCallbacks(request.Operation, callbacks); err != nil {
		return nil, err
	}

	decision, err := d.classifyResourceApply(ctx, request, callbacks)
	if err != nil {
		return nil, err
	}
	if decision != request.Operation.Decision {
		return nil, fmt.Errorf(
			"resource decision changed from %s to %s; create a new plan",
			request.Operation.Decision,
			decision,
		)
	}

	switch decision {
	case DecisionCreate, DecisionUpdate, DecisionReplace:
		if err := d.validateResourceApplyInputs(ctx, request); err != nil {
			return nil, err
		}
	}

	switch decision {
	case DecisionCreate:
		if request.Operation.Prior != nil {
			observation, err := readPriorForApply(ctx, callbacks.ReadPrior)
			if err != nil {
				return nil, err
			}
			if observation.Status == ObservationPresent {
				return nil, fmt.Errorf(
					"remote object reappeared after planning; create a new plan",
				)
			}
		}
		return d.createResourceForApply(ctx, request, callbacks)
	case DecisionUpdate:
		return d.updateResourceForApply(ctx, request, callbacks)
	case DecisionReplace:
		if err := deletePriorForApply(
			ctx,
			*request.Operation.Prior,
			callbacks,
		); err != nil {
			return nil, err
		}
		return d.createResourceForApply(ctx, request, callbacks)
	case DecisionDestroy:
		if err := deletePriorForApply(
			ctx,
			*request.Operation.Prior,
			callbacks,
		); err != nil {
			return nil, err
		}
		if err := persistResourceApplyTarget(ctx, callbacks.Persist, nil); err != nil {
			return nil, err
		}
		return nil, nil
	case DecisionNoOp:
		observation := request.Operation.Observation
		target, err := d.newResourceApplyTarget(
			request,
			*observation.Outputs,
			*observation.Identity,
		)
		if err != nil {
			return nil, err
		}
		if err := persistResourceApplyTarget(ctx, callbacks.Persist, &target); err != nil {
			return nil, err
		}
		return &target, nil
	default:
		return nil, fmt.Errorf("unsupported resource decision %q", decision)
	}
}

func validateResourceApplyPremises[In, Out, Config any](
	request resourceApplyRequest[In, Out, Config],
) error {
	operation := request.Operation
	if err := validatePlanDependencies(request.DependsOn); err != nil {
		return err
	}
	if operation.Desired == nil {
		if request.Desired != nil {
			if operation.Decision == DecisionDestroy {
				return fmt.Errorf("saved destroy requires the resource to remain absent")
			}
			return fmt.Errorf("saved operation forbids a desired resource")
		}
	} else {
		if request.Desired == nil {
			return fmt.Errorf("saved operation requires a desired resource")
		}
		if err := validateResourceApplyDesired(*operation.Desired, request.Desired.Target); err != nil {
			return err
		}
	}

	if operation.Prior == nil {
		if request.Prior != nil {
			return fmt.Errorf("saved operation forbids a prior target")
		}
	} else {
		if request.Prior == nil {
			return fmt.Errorf("saved operation requires a prior target")
		}
		if !sameResourceTarget(*operation.Prior, request.Prior.Target) {
			return fmt.Errorf("prior target does not match the saved plan")
		}
	}

	if operation.Observation == nil {
		if request.Observation != nil {
			return fmt.Errorf("saved operation forbids a resource observation")
		}
	} else {
		if request.Observation == nil {
			return fmt.Errorf("saved operation requires a resource observation")
		}
		if !sameResourceObservation(
			*operation.Observation,
			request.Observation.Observation,
		) {
			return fmt.Errorf("resource observation does not match the saved plan")
		}
		if err := validatePreparedResourceObservation(*request.Observation); err != nil {
			return err
		}
	}
	return nil
}

func validateResourceApplyDesired(
	planned PlannedResourceTarget,
	current PlannedResourceTarget,
) error {
	if err := current.Validate(); err != nil {
		return fmt.Errorf("current desired target: %w", err)
	}
	if current.Inputs.HasPending() {
		return fmt.Errorf("desired inputs did not resolve before apply")
	}
	if current.Configuration.Kind != PlannedConfigurationConcrete {
		return fmt.Errorf("desired configuration did not resolve before apply")
	}
	if planned.Binding != current.Binding {
		return fmt.Errorf("desired binding does not match the saved plan")
	}
	if !plannedEncodedValueMatches(planned.Inputs, current.Inputs) {
		return fmt.Errorf("desired inputs do not match the saved plan")
	}
	if !slices.Equal(planned.SensitiveInputPaths, current.SensitiveInputPaths) {
		return fmt.Errorf("sensitive input paths do not match the saved plan")
	}
	if !slices.Equal(planned.SensitiveOutputPaths, current.SensitiveOutputPaths) {
		return fmt.Errorf("sensitive output paths do not match the saved plan")
	}
	if planned.Configuration.Kind == PlannedConfigurationConcrete &&
		!sameConfigurationRecord(*planned.Configuration.Record, *current.Configuration.Record) {
		return fmt.Errorf("desired configuration does not match the saved plan")
	}
	return nil
}

func validatePreparedResourceObservation[Out any](
	prepared preparedResourceObservation[Out],
) error {
	if err := prepared.Observation.Validate(); err != nil {
		return fmt.Errorf("prepared resource observation: %w", err)
	}
	isNil := resourceApplyOutputIsNil(prepared.Outputs)
	switch prepared.Observation.Status {
	case ObservationPresent:
		if isNil {
			return fmt.Errorf("present prepared observation requires typed outputs")
		}
	case ObservationAbsent:
		if !isNil {
			return fmt.Errorf("absent prepared observation forbids typed outputs")
		}
	}
	return nil
}

func validateResourceApplyCallbacks[In, Out, Config any](
	operation ResourcePlanOperation,
	callbacks resourceApplyCallbacks[In, Out, Config],
) error {
	if callbacks.Persist == nil {
		return fmt.Errorf("resource state persistence callback is required")
	}
	switch operation.Decision {
	case DecisionCreate:
		if callbacks.Create == nil {
			return fmt.Errorf("resource create callback is required")
		}
		if operation.Prior != nil && callbacks.ReadPrior == nil {
			return fmt.Errorf("prior resource read callback is required")
		}
	case DecisionUpdate:
		if callbacks.Update == nil {
			return fmt.Errorf("resource update callback is required")
		}
	case DecisionReplace:
		if callbacks.ReadPrior == nil {
			return fmt.Errorf("prior resource read callback is required")
		}
		if callbacks.DeletePrior == nil {
			return fmt.Errorf("prior resource delete callback is required")
		}
		if callbacks.Create == nil {
			return fmt.Errorf("resource create callback is required")
		}
	case DecisionDestroy:
		if callbacks.ReadPrior == nil {
			return fmt.Errorf("prior resource read callback is required")
		}
		if callbacks.DeletePrior == nil {
			return fmt.Errorf("prior resource delete callback is required")
		}
	}
	return nil
}

func (d resolvedResourceDefinition[In, Out, Config]) classifyResourceApply(
	ctx context.Context,
	request resourceApplyRequest[In, Out, Config],
	callbacks resourceApplyCallbacks[In, Out, Config],
) (Decision, error) {
	if request.Desired == nil {
		return DecisionDestroy, nil
	}
	if request.Prior == nil {
		return DecisionCreate, nil
	}
	if request.Observation.Observation.Status == ObservationAbsent {
		return DecisionCreate, nil
	}
	if request.Desired.Target.Binding != request.Prior.Target.Binding {
		return DecisionReplace, nil
	}

	var desiredObservation *preparedResourceObservation[Out]
	configurationChanged := request.Desired.Target.Configuration.Record.Digest !=
		request.Prior.Target.Configuration.Digest
	if configurationChanged && d.identityScope == IdentityGlobal {
		if request.Operation.Desired.Configuration.Kind == PlannedConfigurationPending {
			if callbacks.ReadDesired == nil {
				return "", fmt.Errorf("desired configuration read callback is required")
			}
			observed, err := d.readDesiredResourceForApply(ctx, request, callbacks.ReadDesired)
			if err != nil {
				return "", err
			}
			desiredObservation = &observed
		} else {
			desiredObservation = request.Observation
		}
	}

	operation, err := d.planPreparedResourceOperation(preparedResourcePlanningRequest[In, Out]{
		Desired:                         request.Desired,
		Prior:                           request.Prior,
		RecordedObservation:             request.Observation,
		DesiredConfigurationObservation: desiredObservation,
	})
	if err != nil {
		return "", err
	}
	if operation == nil {
		return "", fmt.Errorf("resource apply classification produced no operation")
	}
	return operation.Decision, nil
}

func (d resolvedResourceDefinition[In, Out, Config]) readDesiredResourceForApply(
	ctx context.Context,
	request resourceApplyRequest[In, Out, Config],
	read func(
		context.Context,
		In,
		Config,
		Out,
	) (preparedResourceApplyResult[Out], error),
) (preparedResourceObservation[Out], error) {
	result, err := guard(
		"reading this resource with the desired configuration",
		false,
		func() (preparedResourceApplyResult[Out], error) {
			return read(
				ctx,
				request.Prior.Inputs,
				request.DesiredConfiguration,
				request.Prior.Outputs,
			)
		},
	)
	if errors.Is(err, ErrNotFound) {
		return preparedResourceObservation[Out]{
			Observation: ResourceObservation{Status: ObservationAbsent},
		}, nil
	}
	if err != nil {
		return preparedResourceObservation[Out]{}, fmt.Errorf(
			"read resource with desired configuration: %w",
			err,
		)
	}
	if err := validatePreparedResourceApplyResult(result); err != nil {
		return preparedResourceObservation[Out]{}, err
	}
	identity, err := d.newIdentityRecord(
		request.Desired.Target.Binding.LibraryPath,
		request.Desired.Target.Binding.Export,
		request.Prior.Inputs,
		result.Outputs,
	)
	if err != nil {
		return preparedResourceObservation[Out]{}, err
	}
	observation := ResourceObservation{
		Status:   ObservationPresent,
		Outputs:  new(result.Encoded),
		Identity: new(identity),
	}
	return preparedResourceObservation[Out]{
		Observation: observation,
		Outputs:     result.Outputs,
	}, nil
}

func (d resolvedResourceDefinition[In, Out, Config]) validateResourceApplyInputs(
	ctx context.Context,
	request resourceApplyRequest[In, Out, Config],
) error {
	if d.validate == nil {
		return nil
	}
	err := guardErr("validating this resource's inputs", false, func() error {
		return d.validate(
			ctx,
			request.Desired.Inputs,
			request.DesiredConfiguration,
		)
	})
	if err != nil {
		return fmt.Errorf("validate resource inputs: %w", err)
	}
	return nil
}

func (d resolvedResourceDefinition[In, Out, Config]) createResourceForApply(
	ctx context.Context,
	request resourceApplyRequest[In, Out, Config],
	callbacks resourceApplyCallbacks[In, Out, Config],
) (*ResourceTarget, error) {
	result, err := guard(
		"creating this resource",
		false,
		func() (preparedResourceApplyResult[Out], error) {
			return callbacks.Create(
				ctx,
				request.Desired.Inputs,
				request.DesiredConfiguration,
			)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}
	if err := validatePreparedResourceApplyResult(result); err != nil {
		return nil, err
	}
	identity, err := d.newIdentityRecord(
		request.Desired.Target.Binding.LibraryPath,
		request.Desired.Target.Binding.Export,
		request.Desired.Inputs,
		result.Outputs,
	)
	if err != nil {
		return nil, err
	}
	target, err := d.newResourceApplyTarget(request, result.Encoded, identity)
	if err != nil {
		return nil, err
	}
	if err := persistResourceApplyTarget(ctx, callbacks.Persist, &target); err != nil {
		return nil, err
	}
	return &target, nil
}

func (d resolvedResourceDefinition[In, Out, Config]) updateResourceForApply(
	ctx context.Context,
	request resourceApplyRequest[In, Out, Config],
	callbacks resourceApplyCallbacks[In, Out, Config],
) (*ResourceTarget, error) {
	prior := Prior[In, Out]{
		Inputs:   request.Prior.Inputs,
		Outputs:  request.Prior.Outputs,
		Observed: request.Observation.Outputs,
	}
	result, err := guard(
		"updating this resource",
		false,
		func() (preparedResourceApplyResult[Out], error) {
			return callbacks.Update(
				ctx,
				request.Desired.Inputs,
				request.DesiredConfiguration,
				prior,
			)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("update resource: %w", err)
	}
	if err := validatePreparedResourceApplyResult(result); err != nil {
		return nil, err
	}
	identity, err := d.validateUpdatedIdentity(
		request.Desired.Target.Binding.LibraryPath,
		request.Desired.Target.Binding.Export,
		request.Desired.Inputs,
		result.Outputs,
		request.Prior.Target.Identity,
	)
	if err != nil {
		return nil, err
	}
	target, err := d.newResourceApplyTarget(request, result.Encoded, identity)
	if err != nil {
		return nil, err
	}
	if err := persistResourceApplyTarget(ctx, callbacks.Persist, &target); err != nil {
		return nil, err
	}
	return &target, nil
}

func (d resolvedResourceDefinition[In, Out, Config]) newResourceApplyTarget(
	request resourceApplyRequest[In, Out, Config],
	outputs EncodedValue,
	identity IdentityRecord,
) (ResourceTarget, error) {
	configuration := internalconfig.Clone(*request.Desired.Target.Configuration.Record)
	target := ResourceTarget{
		Binding:              request.Desired.Target.Binding,
		SchemaVersion:        d.schemaVersion,
		Inputs:               request.Desired.Target.Inputs,
		Outputs:              outputs,
		Configuration:        configuration,
		Identity:             cloneIdentityRecord(identity),
		DependsOn:            slices.Clone(request.DependsOn),
		SensitiveInputPaths:  slices.Clone(request.Desired.Target.SensitiveInputPaths),
		SensitiveOutputPaths: slices.Clone(request.Desired.Target.SensitiveOutputPaths),
	}
	if err := target.Validate(); err != nil {
		return ResourceTarget{}, fmt.Errorf("next resource target: %w", err)
	}
	return target, nil
}

func readPriorForApply(
	ctx context.Context,
	read func(context.Context) (ResourceObservation, error),
) (ResourceObservation, error) {
	observation, err := guard(
		"reading the prior resource before deletion",
		false,
		func() (ResourceObservation, error) {
			return read(ctx)
		},
	)
	if errors.Is(err, ErrNotFound) {
		return ResourceObservation{Status: ObservationAbsent}, nil
	}
	if err != nil {
		return ResourceObservation{}, fmt.Errorf("read prior resource: %w", err)
	}
	if err := observation.Validate(); err != nil {
		return ResourceObservation{}, fmt.Errorf("fresh resource observation: %w", err)
	}
	return observation, nil
}

func deletePriorForApply[In, Out, Config any](
	ctx context.Context,
	prior ResourceTarget,
	callbacks resourceApplyCallbacks[In, Out, Config],
) error {
	observation, err := readPriorForApply(ctx, callbacks.ReadPrior)
	if err != nil {
		return err
	}
	if observation.Status == ObservationAbsent {
		return nil
	}
	if err := validatePlanningIdentity(prior.Identity, observation.Identity, "fresh"); err != nil {
		return err
	}
	err = guardErr("deleting the prior resource", false, func() error {
		return callbacks.DeletePrior(ctx, *observation.Outputs)
	})
	if err != nil {
		return fmt.Errorf("delete prior resource: %w", err)
	}
	return nil
}

func persistResourceApplyTarget(
	ctx context.Context,
	persist func(context.Context, *ResourceTarget) error,
	target *ResourceTarget,
) error {
	if err := persist(ctx, target); err != nil {
		return fmt.Errorf("persist resource state: %w", err)
	}
	return nil
}

func validatePreparedResourceApplyResult[Out any](
	result preparedResourceApplyResult[Out],
) error {
	if resourceApplyOutputIsNil(result.Outputs) {
		return fmt.Errorf("resource operation returned nil outputs")
	}
	if err := validatePlanObject(result.Encoded, "resource outputs", false); err != nil {
		return err
	}
	return nil
}

func resourceApplyOutputIsNil[Out any](outputs Out) bool {
	value := reflect.ValueOf(outputs)
	return !value.IsValid() || value.Kind() == reflect.Pointer && value.IsNil()
}

func plannedEncodedValueMatches(planned, current EncodedValue) bool {
	if planned.Kind() == EncodedValuePending {
		return !current.HasPending()
	}
	if planned.Kind() != current.Kind() {
		return false
	}
	switch planned.Kind() {
	case EncodedValueAbsent, EncodedValueNull:
		return true
	case EncodedValueBoolean, EncodedValueString, EncodedValueInteger, EncodedValueNumber:
		return encodedValuesEqual(planned, current)
	case EncodedValueList:
		plannedItems, _ := planned.Items()
		currentItems, _ := current.Items()
		if len(plannedItems) != len(currentItems) {
			return false
		}
		for i := range plannedItems {
			if !plannedEncodedValueMatches(plannedItems[i], currentItems[i]) {
				return false
			}
		}
		return true
	case EncodedValueMap:
		plannedEntries, _ := planned.MapEntries()
		currentEntries, _ := current.MapEntries()
		return plannedNamedValuesMatch(plannedEntries, currentEntries)
	case EncodedValueObject:
		plannedFields, _ := planned.ObjectFields()
		currentFields, _ := current.ObjectFields()
		return plannedNamedValuesMatch(plannedFields, currentFields)
	default:
		return false
	}
}

func plannedNamedValuesMatch(
	planned map[string]EncodedValue,
	current map[string]EncodedValue,
) bool {
	if len(planned) != len(current) {
		return false
	}
	for name, plannedValue := range planned {
		currentValue, ok := current[name]
		if !ok || !plannedEncodedValueMatches(plannedValue, currentValue) {
			return false
		}
	}
	return true
}

func sameResourceTarget(a, b ResourceTarget) bool {
	return a.Binding == b.Binding &&
		a.SchemaVersion == b.SchemaVersion &&
		encodedValuesEqual(a.Inputs, b.Inputs) &&
		encodedValuesEqual(a.Outputs, b.Outputs) &&
		sameConfigurationRecord(a.Configuration, b.Configuration) &&
		sameIdentityRecord(a.Identity, b.Identity) &&
		slices.Equal(a.DependsOn, b.DependsOn) &&
		slices.Equal(a.SensitiveInputPaths, b.SensitiveInputPaths) &&
		slices.Equal(a.SensitiveOutputPaths, b.SensitiveOutputPaths)
}

func sameConfigurationRecord(a, b ConfigurationRecord) bool {
	return a.Address == b.Address &&
		a.LibraryPath == b.LibraryPath &&
		a.SchemaVersion == b.SchemaVersion &&
		a.SchemaDigest == b.SchemaDigest &&
		encodedValuesEqual(a.Value, b.Value) &&
		slices.Equal(a.SensitivePaths, b.SensitivePaths) &&
		slices.Equal(a.SensitiveValues, b.SensitiveValues) &&
		a.Digest == b.Digest
}

func sameIdentityRecord(a, b IdentityRecord) bool {
	return a.DefinitionDigest == b.DefinitionDigest &&
		a.Version == b.Version &&
		sameString(a.StableID, b.StableID)
}

func sameResourceObservation(a, b ResourceObservation) bool {
	if a.Status != b.Status || a.Outputs == nil != (b.Outputs == nil) ||
		a.Identity == nil != (b.Identity == nil) {
		return false
	}
	if a.Outputs != nil && !encodedValuesEqual(*a.Outputs, *b.Outputs) {
		return false
	}
	return a.Identity == nil || sameIdentityRecord(*a.Identity, *b.Identity)
}

func cloneIdentityRecord(identity IdentityRecord) IdentityRecord {
	identity.StableID = copyString(identity.StableID)
	return identity
}
