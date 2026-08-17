package runtime

import (
	"fmt"
	"slices"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
)

func (d resolvedResourceDefinition[In, Out, Config]) prepareResourcePlanningRequest(
	request resourcePlanningRequest,
) (preparedResourcePlanningRequest[In, Out], error) {
	var prepared preparedResourcePlanningRequest[In, Out]

	desired, err := prepareResourceDesired[In](request.Desired)
	if err != nil {
		return prepared, err
	}
	prepared.Desired = desired

	prior, err := d.prepareResourcePrior(request.Prior)
	if err != nil {
		return prepared, err
	}
	prepared.Prior = prior

	observation, err := prepareResourceObservation[Out](
		"recorded-target",
		request.RecordedObservation,
	)
	if err != nil {
		return prepared, err
	}
	prepared.RecordedObservation = observation

	desiredObservation, err := prepareResourceObservation[Out](
		"desired-configuration",
		request.DesiredConfigurationObservation,
	)
	if err != nil {
		return prepared, err
	}
	prepared.DesiredConfigurationObservation = desiredObservation
	return prepared, nil
}

func prepareResourceDesired[In any](
	target *PlannedResourceTarget,
) (*preparedResourceDesired[In], error) {
	if target == nil {
		return nil, nil
	}
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("desired target: %w", err)
	}
	inputs, err := decodeResourceInputValue[In](target.Inputs, true)
	if err != nil {
		return nil, fmt.Errorf("desired resource inputs: %w", err)
	}
	return &preparedResourceDesired[In]{
		Target: clonePlannedResourceTarget(*target),
		Inputs: inputs,
	}, nil
}

func (d resolvedResourceDefinition[In, Out, Config]) prepareResourcePrior(
	target *ResourceTarget,
) (*preparedResourcePrior[In, Out], error) {
	if target == nil {
		return nil, nil
	}
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("prior target: %w", err)
	}

	preparedTarget := cloneResourceTarget(*target)
	resource := ResourceMigrationState{
		Inputs:  preparedTarget.Inputs,
		Outputs: preparedTarget.Outputs,
	}
	switch {
	case preparedTarget.SchemaVersion > d.schemaVersion:
		return nil, fmt.Errorf(
			"recorded resource schema version %d is newer than registered version %d",
			preparedTarget.SchemaVersion,
			d.schemaVersion,
		)
	case preparedTarget.SchemaVersion < d.schemaVersion:
		if d.migrate == nil {
			return nil, fmt.Errorf(
				"no resource migration registered for version %d",
				preparedTarget.SchemaVersion,
			)
		}
		migrated, err := guard(
			"migrating this resource's state",
			false,
			func() (ResourceMigrationState, error) {
				return d.migrate(preparedTarget.SchemaVersion, resource)
			},
		)
		if err != nil {
			return nil, err
		}
		resource = migrated
		preparedTarget.SchemaVersion = d.schemaVersion
		preparedTarget.Inputs = resource.Inputs
		preparedTarget.Outputs = resource.Outputs
	}

	inputs, err := decodeResourceInputs[In](resource.Inputs)
	if err != nil {
		return nil, fmt.Errorf("recorded resource inputs: %w", err)
	}
	outputs, err := decodeResourceOutputs[Out](resource.Outputs)
	if err != nil {
		return nil, fmt.Errorf("recorded resource outputs: %w", err)
	}
	identity, err := d.migrateIdentityRecord(
		preparedTarget.Binding.LibraryPath,
		preparedTarget.Binding.Export,
		resource,
		preparedTarget.Identity,
	)
	if err != nil {
		return nil, err
	}
	preparedTarget.Identity = identity
	if err := preparedTarget.Validate(); err != nil {
		return nil, fmt.Errorf("prepared prior target: %w", err)
	}
	return &preparedResourcePrior[In, Out]{
		Target:  preparedTarget,
		Inputs:  inputs,
		Outputs: outputs,
	}, nil
}

func prepareResourceObservation[Out any](
	name string,
	observation *ResourceObservation,
) (*preparedResourceObservation[Out], error) {
	if observation == nil {
		return nil, nil
	}
	cloned := cloneResourceObservation(*observation)
	if err := cloned.Validate(); err != nil {
		return nil, fmt.Errorf("%s observation: %w", name, err)
	}
	prepared := &preparedResourceObservation[Out]{Observation: cloned}
	if cloned.Status == ObservationAbsent {
		return prepared, nil
	}
	outputs, err := decodeResourceOutputs[Out](*cloned.Outputs)
	if err != nil {
		return nil, fmt.Errorf("%s observation outputs: %w", name, err)
	}
	prepared.Outputs = outputs
	return prepared, nil
}

func clonePlannedResourceTarget(target PlannedResourceTarget) PlannedResourceTarget {
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

func cloneResourceTarget(target ResourceTarget) ResourceTarget {
	result := target
	result.Configuration = internalconfig.Clone(target.Configuration)
	result.Identity = cloneIdentityRecord(target.Identity)
	result.DependsOn = slices.Clone(target.DependsOn)
	result.SensitiveInputPaths = slices.Clone(target.SensitiveInputPaths)
	result.SensitiveOutputPaths = slices.Clone(target.SensitiveOutputPaths)
	return result
}
