package runtime

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

type resourceRegistrationConfig struct {
	Region string
}

type resourceRegistrationCapture struct {
	calls         []string
	name          string
	size          int
	configuration resourceRegistrationConfig
	createResult  *resourceRegistrationOutput
	readResult    *resourceRegistrationOutput
	updateResult  *resourceRegistrationOutput
	updatePrior   Prior[resourceRegistrationInput, *resourceRegistrationOutput]
}

type resourceRegistrationInput struct {
	Name    string `ub:"name"`
	Size    int    `ub:"size"`
	capture *resourceRegistrationCapture
}

type resourceRegistrationOutput struct {
	ID   string `ub:"id"`
	Name string `ub:"name"`
}

func (r *resourceRegistrationInput) Create(
	_ context.Context,
	configuration resourceRegistrationConfig,
) (*resourceRegistrationOutput, error) {
	r.capture.calls = append(r.capture.calls, "create")
	r.capture.name = r.Name
	r.capture.size = r.Size
	r.capture.configuration = configuration
	return r.capture.createResult, nil
}

func (r *resourceRegistrationInput) Read(
	context.Context,
	resourceRegistrationConfig,
	*resourceRegistrationOutput,
) (*resourceRegistrationOutput, error) {
	r.capture.calls = append(r.capture.calls, "read")
	if r.capture.readResult != nil {
		return r.capture.readResult, nil
	}
	return r.capture.createResult, nil
}

func (r *resourceRegistrationInput) Update(
	_ context.Context,
	configuration resourceRegistrationConfig,
	prior Prior[resourceRegistrationInput, *resourceRegistrationOutput],
) (*resourceRegistrationOutput, error) {
	r.capture.calls = append(r.capture.calls, "update")
	r.capture.name = r.Name
	r.capture.size = r.Size
	r.capture.configuration = configuration
	r.capture.updatePrior = prior
	if r.capture.updateResult != nil {
		return r.capture.updateResult, nil
	}
	return r.capture.createResult, nil
}

func (r *resourceRegistrationInput) Delete(
	context.Context,
	resourceRegistrationConfig,
	*resourceRegistrationOutput,
) error {
	r.capture.calls = append(r.capture.calls, "delete")
	return nil
}

func resourceRegistrationDefinition() ResourceDefinition[
	resourceRegistrationInput,
	*resourceRegistrationOutput,
	resourceRegistrationConfig,
] {
	name := InputField(func(value *resourceRegistrationInput) *string {
		return &value.Name
	})
	return ResourceDefinition[
		resourceRegistrationInput,
		*resourceRegistrationOutput,
		resourceRegistrationConfig,
	]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[
			resourceRegistrationInput,
			*resourceRegistrationOutput,
		]{
			Version:       1,
			Scope:         IdentityConfiguration,
			AddressInputs: []AnyInputField[resourceRegistrationInput]{name},
			StableID: func(
				_ resourceRegistrationInput,
				outputs *resourceRegistrationOutput,
			) (string, error) {
				return outputs.ID, nil
			},
		},
	}
}

func registrationConfigurationRecord(t *testing.T, libraryPath string) ConfigurationRecord {
	t.Helper()
	definition, err := resolveConfigurationDefinition(libraryPath, nil)
	require.NoError(t, err)
	empty, err := ObjectValue(nil)
	require.NoError(t, err)
	record, err := definition.newConfigurationRecord("", empty, nil, nil)
	require.NoError(t, err)
	return record
}

func TestResourceDefinitionRegistrationPlansAndAppliesCreate(t *testing.T) {
	capture := &resourceRegistrationCapture{
		createResult: &resourceRegistrationOutput{ID: "object-1", Name: "logs"},
	}
	definition := resourceRegistrationDefinition()
	definition.Validate = func(
		_ context.Context,
		inputs resourceRegistrationInput,
		configuration resourceRegistrationConfig,
	) error {
		capture.calls = append(capture.calls, "validate")
		capture.name = inputs.Name
		capture.configuration = configuration
		return nil
	}
	registration, err := newResourceDefinitionRegistration[
		resourceRegistrationInput,
		*resourceRegistrationOutput,
		resourceRegistrationConfig,
		*resourceRegistrationInput,
	](definition, func() *resourceRegistrationInput {
		return &resourceRegistrationInput{capture: capture}
	})
	require.NoError(t, err)

	inputs, err := registration.prepareInputs(map[string]any{"name": "logs", "size": 1})
	require.NoError(t, err)
	configuration := registrationConfigurationRecord(t, "example.com/current")
	desired := PlannedResourceTarget{
		Binding: Binding{
			LibraryPath: "example.com/current",
			Export:      "bucket",
		},
		Inputs: inputs,
		Configuration: PlannedConfiguration{
			Kind:   PlannedConfigurationConcrete,
			Record: &configuration,
		},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	operation, err := registration.planResourceOperation(resourcePlanningRequest{
		Desired: &desired,
	})
	require.NoError(t, err)
	require.Equal(t, DecisionCreate, operation.Decision)

	var persisted *ResourceTarget
	target, err := registration.applyResourceOperation(
		context.Background(),
		resourceRegistrationApplyRequest{
			Address:              "resource.logs",
			Operation:            *operation,
			Desired:              &desired,
			DesiredConfiguration: resourceRegistrationConfig{Region: "us-east-1"},
			DependsOn:            []string{"resource.network"},
		},
		resourceRegistrationApplyCallbacks{
			Persist: func(_ context.Context, target *ResourceTarget) error {
				persisted = target
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"validate", "create"}, capture.calls)
	require.Equal(t, "logs", capture.name)
	require.Equal(t, 1, capture.size)
	require.Equal(t, "us-east-1", capture.configuration.Region)
	require.Equal(t, target, persisted)
	require.Equal(t, "object-1", stringValue(target.Identity.StableID))
	require.Equal(t, []string{"resource.network"}, target.DependsOn)
}

func TestResourceDefinitionRegistrationAppliesUpdateFromSavedObservation(t *testing.T) {
	capture := &resourceRegistrationCapture{
		createResult: &resourceRegistrationOutput{ID: "object-1", Name: "recorded"},
		readResult:   &resourceRegistrationOutput{ID: "object-1", Name: "observed"},
		updateResult: &resourceRegistrationOutput{ID: "object-1", Name: "updated"},
	}
	registration, err := newResourceDefinitionRegistration[
		resourceRegistrationInput,
		*resourceRegistrationOutput,
		resourceRegistrationConfig,
		*resourceRegistrationInput,
	](resourceRegistrationDefinition(), func() *resourceRegistrationInput {
		return &resourceRegistrationInput{capture: capture}
	})
	require.NoError(t, err)

	priorInputs, err := registration.prepareInputs(map[string]any{"name": "logs", "size": 1})
	require.NoError(t, err)
	priorOutputs, err := encodeResourceOutputs(capture.createResult)
	require.NoError(t, err)
	configuration := registrationConfigurationRecord(t, "example.com/current")
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	readRequest := resourceReadRequest{
		Address:       "resource.logs",
		Binding:       binding,
		Inputs:        priorInputs,
		Configuration: configuration,
		PriorOutputs:  priorOutputs,
	}
	observation, err := registration.readResourceObservation(
		context.Background(),
		readRequest,
		resourceRegistrationConfig{Region: "us-east-1"},
	)
	require.NoError(t, err)
	priorIdentity, err := resolveResourceDefinition(resourceRegistrationDefinition())
	require.NoError(t, err)
	identity, err := priorIdentity.newIdentityRecord(
		binding.LibraryPath,
		binding.Export,
		resourceRegistrationInput{Name: "logs", Size: 1},
		capture.createResult,
	)
	require.NoError(t, err)
	priorTarget := ResourceTarget{
		Binding:              binding,
		SchemaVersion:        1,
		Inputs:               priorInputs,
		Outputs:              priorOutputs,
		Configuration:        configuration,
		Identity:             identity,
		DependsOn:            []string{},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	desiredInputs, err := registration.prepareInputs(map[string]any{"name": "logs", "size": 2})
	require.NoError(t, err)
	desiredTarget := PlannedResourceTarget{
		Binding: binding,
		Inputs:  desiredInputs,
		Configuration: PlannedConfiguration{
			Kind:   PlannedConfigurationConcrete,
			Record: &configuration,
		},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	operation, err := registration.planResourceOperation(resourcePlanningRequest{
		Desired:             &desiredTarget,
		Prior:               &priorTarget,
		RecordedObservation: &observation,
	})
	require.NoError(t, err)
	require.Equal(t, DecisionUpdate, operation.Decision)
	capture.calls = nil

	target, err := registration.applyResourceOperation(
		context.Background(),
		resourceRegistrationApplyRequest{
			Address:              "resource.logs",
			Operation:            *operation,
			Desired:              &desiredTarget,
			DesiredConfiguration: resourceRegistrationConfig{Region: "us-west-2"},
			Prior:                &priorTarget,
			Observation:          &observation,
			DependsOn:            []string{},
		},
		resourceRegistrationApplyCallbacks{
			Persist: func(context.Context, *ResourceTarget) error { return nil },
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"update"}, capture.calls)
	require.Equal(t, "logs", capture.name)
	require.Equal(t, 2, capture.size)
	require.Equal(t, "us-west-2", capture.configuration.Region)
	require.Equal(t, resourceRegistrationInput{Name: "logs", Size: 1}, capture.updatePrior.Inputs)
	require.Equal(t, capture.createResult, capture.updatePrior.Outputs)
	require.Equal(t, capture.readResult, capture.updatePrior.Observed)
	require.Equal(t, "object-1", stringValue(target.Identity.StableID))
}

type priorRegistrationInput struct {
	Name    int `ub:"name"`
	capture *bindingRegistrationCapture
}

type priorRegistrationOutput struct {
	ID int `ub:"id"`
}

type desiredRegistrationInput struct {
	Name    string `ub:"name"`
	capture *bindingRegistrationCapture
}

type desiredRegistrationOutput struct {
	ID string `ub:"id"`
}

type bindingRegistrationCapture struct {
	calls        []string
	deletedID    int
	createdName  string
	createdValue *desiredRegistrationOutput
}

func (r *priorRegistrationInput) Create(
	context.Context,
	NoConfig,
) (*priorRegistrationOutput, error) {
	return &priorRegistrationOutput{ID: r.Name}, nil
}

func (r *priorRegistrationInput) Read(
	_ context.Context,
	_ NoConfig,
	prior *priorRegistrationOutput,
) (*priorRegistrationOutput, error) {
	r.capture.calls = append(r.capture.calls, "prior-read")
	return &priorRegistrationOutput{ID: prior.ID}, nil
}

func (r *priorRegistrationInput) Update(
	context.Context,
	NoConfig,
	Prior[priorRegistrationInput, *priorRegistrationOutput],
) (*priorRegistrationOutput, error) {
	return &priorRegistrationOutput{ID: r.Name}, nil
}

func (r *priorRegistrationInput) Delete(
	_ context.Context,
	_ NoConfig,
	prior *priorRegistrationOutput,
) error {
	r.capture.calls = append(r.capture.calls, "prior-delete")
	r.capture.deletedID = prior.ID
	return nil
}

func (r *desiredRegistrationInput) Create(
	context.Context,
	NoConfig,
) (*desiredRegistrationOutput, error) {
	r.capture.calls = append(r.capture.calls, "desired-create")
	r.capture.createdName = r.Name
	return r.capture.createdValue, nil
}

func (r *desiredRegistrationInput) Read(
	_ context.Context,
	_ NoConfig,
	prior *desiredRegistrationOutput,
) (*desiredRegistrationOutput, error) {
	return prior, nil
}

func (r *desiredRegistrationInput) Update(
	context.Context,
	NoConfig,
	Prior[desiredRegistrationInput, *desiredRegistrationOutput],
) (*desiredRegistrationOutput, error) {
	return r.capture.createdValue, nil
}

func (r *desiredRegistrationInput) Delete(
	context.Context,
	NoConfig,
	*desiredRegistrationOutput,
) error {
	return nil
}

func priorResourceRegistrationDefinition() ResourceDefinition[
	priorRegistrationInput,
	*priorRegistrationOutput,
	NoConfig,
] {
	name := InputField(func(value *priorRegistrationInput) *int { return &value.Name })
	return ResourceDefinition[priorRegistrationInput, *priorRegistrationOutput, NoConfig]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[priorRegistrationInput, *priorRegistrationOutput]{
			Version:       1,
			Scope:         IdentityConfiguration,
			AddressInputs: []AnyInputField[priorRegistrationInput]{name},
			StableID: func(
				_ priorRegistrationInput,
				outputs *priorRegistrationOutput,
			) (string, error) {
				return strconv.Itoa(outputs.ID), nil
			},
		},
	}
}

func desiredResourceRegistrationDefinition() ResourceDefinition[
	desiredRegistrationInput,
	*desiredRegistrationOutput,
	NoConfig,
] {
	name := InputField(func(value *desiredRegistrationInput) *string { return &value.Name })
	return ResourceDefinition[desiredRegistrationInput, *desiredRegistrationOutput, NoConfig]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[desiredRegistrationInput, *desiredRegistrationOutput]{
			Version:       1,
			Scope:         IdentityConfiguration,
			AddressInputs: []AnyInputField[desiredRegistrationInput]{name},
			StableID: func(
				_ desiredRegistrationInput,
				outputs *desiredRegistrationOutput,
			) (string, error) {
				return outputs.ID, nil
			},
		},
	}
}

func TestResourceDefinitionRegistrationReplacesUsingPriorImplementation(t *testing.T) {
	capture := &bindingRegistrationCapture{
		createdValue: &desiredRegistrationOutput{ID: "current-1"},
	}
	priorRegistration, err := newResourceDefinitionRegistration[
		priorRegistrationInput,
		*priorRegistrationOutput,
		NoConfig,
		*priorRegistrationInput,
	](priorResourceRegistrationDefinition(), func() *priorRegistrationInput {
		return &priorRegistrationInput{capture: capture}
	})
	require.NoError(t, err)
	desiredRegistration, err := newResourceDefinitionRegistration[
		desiredRegistrationInput,
		*desiredRegistrationOutput,
		NoConfig,
		*desiredRegistrationInput,
	](desiredResourceRegistrationDefinition(), func() *desiredRegistrationInput {
		return &desiredRegistrationInput{capture: capture}
	})
	require.NoError(t, err)

	priorInputs, err := priorRegistration.prepareInputs(map[string]any{"name": 7})
	require.NoError(t, err)
	priorOutputs, err := encodeResourceOutputs(&priorRegistrationOutput{ID: 7})
	require.NoError(t, err)
	priorConfiguration := registrationConfigurationRecord(t, "example.com/prior")
	priorBinding := Binding{LibraryPath: "example.com/prior", Export: "bucket"}
	readRequest := resourceReadRequest{
		Address:       "resource.logs",
		Binding:       priorBinding,
		Inputs:        priorInputs,
		Configuration: priorConfiguration,
		PriorOutputs:  priorOutputs,
	}
	observation, err := priorRegistration.readResourceObservation(
		context.Background(),
		readRequest,
		NoConfig{},
	)
	require.NoError(t, err)
	require.NotNil(t, observation.Identity)
	capture.calls = nil
	priorTarget, err := priorRegistration.preparePrior(ResourceTarget{
		Binding:              priorBinding,
		SchemaVersion:        1,
		Inputs:               priorInputs,
		Outputs:              priorOutputs,
		Configuration:        priorConfiguration,
		Identity:             *observation.Identity,
		DependsOn:            []string{},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	})
	require.NoError(t, err)

	desiredInputs, err := desiredRegistration.prepareInputs(map[string]any{"name": "logs"})
	require.NoError(t, err)
	desiredConfiguration := registrationConfigurationRecord(t, "example.com/current")
	desiredTarget := PlannedResourceTarget{
		Binding: Binding{
			LibraryPath: "example.com/current",
			Export:      "archive",
		},
		Inputs: desiredInputs,
		Configuration: PlannedConfiguration{
			Kind:   PlannedConfigurationConcrete,
			Record: &desiredConfiguration,
		},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	operation, err := desiredRegistration.planResourceOperation(resourcePlanningRequest{
		Desired:             &desiredTarget,
		Prior:               &priorTarget,
		RecordedObservation: &observation,
	})
	require.NoError(t, err)
	require.Equal(t, DecisionReplace, operation.Decision)
	require.Equal(t, []string{"binding"}, operation.Reasons)

	var persisted *ResourceTarget
	target, err := desiredRegistration.applyResourceOperation(
		context.Background(),
		resourceRegistrationApplyRequest{
			Address:              "resource.logs",
			Operation:            *operation,
			Desired:              &desiredTarget,
			DesiredConfiguration: NoConfig{},
			Prior:                &priorTarget,
			Observation:          &observation,
			DependsOn:            []string{},
		},
		resourceRegistrationApplyCallbacks{
			ReadPrior: func(ctx context.Context) (ResourceObservation, error) {
				return priorRegistration.readResourceObservation(ctx, readRequest, NoConfig{})
			},
			DeletePrior: func(ctx context.Context, outputs EncodedValue) error {
				return priorRegistration.deleteResource(
					ctx,
					priorTarget.Inputs,
					NoConfig{},
					outputs,
				)
			},
			Persist: func(_ context.Context, target *ResourceTarget) error {
				persisted = target
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"prior-read", "prior-delete", "desired-create"},
		capture.calls,
	)
	require.Equal(t, 7, capture.deletedID)
	require.Equal(t, "logs", capture.createdName)
	require.Equal(t, "current-1", stringValue(target.Identity.StableID))
	require.Equal(t, desiredTarget.Binding, target.Binding)
	require.Equal(t, target, persisted)
}

func TestResourceDefinitionRegistrationRejectsInvalidDefinition(t *testing.T) {
	definition := resourceRegistrationDefinition()
	definition.Identity.Version = 0

	registration, err := newResourceDefinitionRegistration[
		resourceRegistrationInput,
		*resourceRegistrationOutput,
		resourceRegistrationConfig,
		*resourceRegistrationInput,
	](definition, nil)
	require.ErrorContains(t, err, "identity version must be greater than zero")
	require.Nil(t, registration)
}

func TestResourceDefinitionRegistrationRejectsWrongConfigurationType(t *testing.T) {
	capture := &resourceRegistrationCapture{
		createResult: &resourceRegistrationOutput{ID: "object-1"},
	}
	registration, err := newResourceDefinitionRegistration[
		resourceRegistrationInput,
		*resourceRegistrationOutput,
		resourceRegistrationConfig,
		*resourceRegistrationInput,
	](resourceRegistrationDefinition(), func() *resourceRegistrationInput {
		return &resourceRegistrationInput{capture: capture}
	})
	require.NoError(t, err)
	inputs, err := registration.prepareInputs(map[string]any{"name": "logs", "size": 1})
	require.NoError(t, err)
	outputs, err := encodeResourceOutputs(capture.createResult)
	require.NoError(t, err)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}

	_, err = registration.readResourceObservation(
		context.Background(),
		resourceReadRequest{
			Address:       "resource.logs",
			Binding:       binding,
			Inputs:        inputs,
			Configuration: registrationConfigurationRecord(t, binding.LibraryPath),
			PriorOutputs:  outputs,
		},
		"not a resource configuration",
	)
	require.ErrorContains(t, err, "config type mismatch")
	require.Empty(t, capture.calls)
}
