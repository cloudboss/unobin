package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type registeredPlanningConfig struct {
	Region string
}

type registeredPlanningCapture struct {
	reads []string
	read  func(string, registeredPlanningConfig, *registeredPlanningOutput) (
		*registeredPlanningOutput,
		error,
	)
}

type registeredPlanningInput struct {
	Name string `ub:"name"`
	Size int    `ub:"size"`

	capture      *registeredPlanningCapture
	registration string
}

type registeredPlanningOutput struct {
	ID    string `ub:"id"`
	Value string `ub:"value"`
}

func (r *registeredPlanningInput) Create(
	context.Context,
	registeredPlanningConfig,
) (*registeredPlanningOutput, error) {
	return &registeredPlanningOutput{ID: r.Name, Value: r.Name}, nil
}

func (r *registeredPlanningInput) Read(
	_ context.Context,
	config registeredPlanningConfig,
	prior *registeredPlanningOutput,
) (*registeredPlanningOutput, error) {
	r.capture.reads = append(r.capture.reads, r.registration+":"+config.Region)
	if r.capture.read != nil {
		return r.capture.read(r.registration, config, prior)
	}
	return prior, nil
}

func (r *registeredPlanningInput) Update(
	context.Context,
	registeredPlanningConfig,
	Prior[registeredPlanningInput, *registeredPlanningOutput],
) (*registeredPlanningOutput, error) {
	return &registeredPlanningOutput{ID: r.Name, Value: r.Name}, nil
}

func (*registeredPlanningInput) Delete(
	context.Context,
	registeredPlanningConfig,
	*registeredPlanningOutput,
) error {
	return nil
}

func registeredPlanningDefinition(
	schemaVersion int,
	scope IdentityScope,
) ResourceDefinition[
	registeredPlanningInput,
	*registeredPlanningOutput,
	registeredPlanningConfig,
] {
	name := InputField(func(value *registeredPlanningInput) *string {
		return &value.Name
	})
	return ResourceDefinition[
		registeredPlanningInput,
		*registeredPlanningOutput,
		registeredPlanningConfig,
	]{
		SchemaVersion: schemaVersion,
		Identity: ResourceIdentity[
			registeredPlanningInput,
			*registeredPlanningOutput,
		]{
			Version:       1,
			Scope:         scope,
			AddressInputs: []AnyInputField[registeredPlanningInput]{name},
			StableID: func(
				_ registeredPlanningInput,
				outputs *registeredPlanningOutput,
			) (string, error) {
				return outputs.ID, nil
			},
		},
	}
}

func newRegisteredPlanningResource(
	t *testing.T,
	definition ResourceDefinition[
		registeredPlanningInput,
		*registeredPlanningOutput,
		registeredPlanningConfig,
	],
	capture *registeredPlanningCapture,
	name string,
) *resourceDefinitionRegistration {
	t.Helper()
	registration, err := newResourceDefinitionRegistration[
		registeredPlanningInput,
		*registeredPlanningOutput,
		registeredPlanningConfig,
		*registeredPlanningInput,
	](definition, func() *registeredPlanningInput {
		return &registeredPlanningInput{
			capture:      capture,
			registration: name,
		}
	})
	require.NoError(t, err)
	return registration
}

func registeredPlanningTarget(
	t *testing.T,
	definition ResourceDefinition[
		registeredPlanningInput,
		*registeredPlanningOutput,
		registeredPlanningConfig,
	],
	registration *resourceDefinitionRegistration,
	binding Binding,
	configuration ConfigurationRecord,
	name string,
	size int,
	outputs *registeredPlanningOutput,
) ResourceTarget {
	t.Helper()
	inputs, err := registration.prepareInputs(map[string]any{
		"name": name,
		"size": size,
	})
	require.NoError(t, err)
	encodedOutputs, err := encodeResourceOutputs(outputs)
	require.NoError(t, err)
	resolved, err := resolveResourceDefinition(definition)
	require.NoError(t, err)
	identity, err := resolved.newIdentityRecord(
		binding.LibraryPath,
		binding.Export,
		registeredPlanningInput{Name: name, Size: size},
		outputs,
	)
	require.NoError(t, err)
	target := ResourceTarget{
		Binding:              binding,
		SchemaVersion:        definition.SchemaVersion,
		Inputs:               inputs,
		Outputs:              encodedOutputs,
		Configuration:        configuration,
		Identity:             identity,
		DependsOn:            []string{},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	require.NoError(t, target.Validate())
	return target
}

func registeredPlanningDesired(
	t *testing.T,
	registration *resourceDefinitionRegistration,
	binding Binding,
	configuration ConfigurationRecord,
	name string,
	size int,
) PlannedResourceTarget {
	t.Helper()
	inputs, err := registration.prepareInputs(map[string]any{
		"name": name,
		"size": size,
	})
	require.NoError(t, err)
	target := PlannedResourceTarget{
		Binding: binding,
		Inputs:  inputs,
		Configuration: PlannedConfiguration{
			Kind:   PlannedConfigurationConcrete,
			Record: &configuration,
		},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	require.NoError(t, target.Validate())
	return target
}

func registeredPlanningConfiguration(
	t *testing.T,
	endpoint string,
) ConfigurationRecord {
	t.Helper()
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	fields, ok := testConfigurationValue(t).ObjectFields()
	require.True(t, ok)
	fields["endpoint"] = StringValue(endpoint)
	value, err := ObjectValue(fields)
	require.NoError(t, err)
	record, err := definition.newConfigurationRecord(
		"library-config.cloud",
		value,
		nil,
		nil,
	)
	require.NoError(t, err)
	return record
}

func TestPlanRegisteredResourceOperationHandlesTargetPresence(t *testing.T) {
	operation, err := runFixedPointPlanning(
		context.Background(),
		func(pass *planningPassState) (*ResourcePlanOperation, error) {
			return planRegisteredResourceOperation(
				context.Background(),
				pass,
				registeredResourcePlanningRequest{Address: "resource.logs"},
			)
		},
	)
	require.NoError(t, err)
	require.Nil(t, operation)

	definition := registeredPlanningDefinition(1, IdentityConfiguration)
	capture := &registeredPlanningCapture{}
	registration := newRegisteredPlanningResource(t, definition, capture, "current")
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configuration := registrationConfigurationRecord(t, binding.LibraryPath)
	desired := registeredPlanningDesired(
		t,
		registration,
		binding,
		configuration,
		"logs",
		1,
	)
	operation, err = runFixedPointPlanning(
		context.Background(),
		func(pass *planningPassState) (*ResourcePlanOperation, error) {
			return planRegisteredResourceOperation(
				context.Background(),
				pass,
				registeredResourcePlanningRequest{
					Address:             "resource.logs",
					Desired:             &desired,
					DesiredRegistration: registration,
				},
			)
		},
	)
	require.NoError(t, err)
	require.Equal(t, DecisionCreate, operation.Decision)
	require.Empty(t, capture.reads)

	prior := registeredPlanningTarget(
		t,
		definition,
		registration,
		binding,
		configuration,
		"logs",
		1,
		&registeredPlanningOutput{ID: "object-1", Value: "recorded"},
	)
	operation, err = runFixedPointPlanning(
		context.Background(),
		func(pass *planningPassState) (*ResourcePlanOperation, error) {
			return planRegisteredResourceOperation(
				context.Background(),
				pass,
				registeredResourcePlanningRequest{
					Address:            "resource.logs",
					Prior:              &prior,
					PriorConfiguration: registeredPlanningConfig{Region: "current"},
					PriorRegistration:  registration,
				},
			)
		},
	)
	require.NoError(t, err)
	require.Equal(t, DecisionDestroy, operation.Decision)
	require.Equal(t, []string{"current:current"}, capture.reads)
}

func TestPlanRegisteredResourceOperationUsesPriorRegistrationForReplacement(t *testing.T) {
	capture := &registeredPlanningCapture{}
	priorDefinition := registeredPlanningDefinition(2, IdentityConfiguration)
	desiredDefinition := registeredPlanningDefinition(1, IdentityConfiguration)
	priorRegistration := newRegisteredPlanningResource(
		t,
		priorDefinition,
		capture,
		"prior",
	)
	desiredRegistration := newRegisteredPlanningResource(
		t,
		desiredDefinition,
		capture,
		"desired",
	)
	priorBinding := Binding{LibraryPath: "example.com/prior", Export: "bucket"}
	desiredBinding := Binding{LibraryPath: "example.com/current", Export: "archive"}
	prior := registeredPlanningTarget(
		t,
		priorDefinition,
		priorRegistration,
		priorBinding,
		registrationConfigurationRecord(t, priorBinding.LibraryPath),
		"old",
		1,
		&registeredPlanningOutput{ID: "object-1", Value: "recorded"},
	)
	desired := registeredPlanningDesired(
		t,
		desiredRegistration,
		desiredBinding,
		registrationConfigurationRecord(t, desiredBinding.LibraryPath),
		"new",
		2,
	)

	operation, err := runFixedPointPlanning(
		context.Background(),
		func(pass *planningPassState) (*ResourcePlanOperation, error) {
			return planRegisteredResourceOperation(
				context.Background(),
				pass,
				registeredResourcePlanningRequest{
					Address:              "resource.logs",
					Desired:              &desired,
					DesiredConfiguration: registeredPlanningConfig{Region: "new"},
					DesiredRegistration:  desiredRegistration,
					Prior:                &prior,
					PriorConfiguration:   registeredPlanningConfig{Region: "old"},
					PriorRegistration:    priorRegistration,
				},
			)
		},
	)
	require.NoError(t, err)
	require.Equal(t, DecisionReplace, operation.Decision)
	require.Equal(t, []string{"binding"}, operation.Reasons)
	require.Equal(t, 2, operation.Prior.SchemaVersion)
	require.Equal(t, []string{"prior:old"}, capture.reads)
}

func TestPlanRegisteredResourceOperationReadsGlobalIdentityWithDesiredConfig(t *testing.T) {
	capture := &registeredPlanningCapture{}
	capture.read = func(
		_ string,
		config registeredPlanningConfig,
		_ *registeredPlanningOutput,
	) (*registeredPlanningOutput, error) {
		return &registeredPlanningOutput{
			ID:    "object-1",
			Value: "observed-" + config.Region,
		}, nil
	}
	definition := registeredPlanningDefinition(1, IdentityGlobal)
	registration := newRegisteredPlanningResource(t, definition, capture, "shared")
	binding := Binding{LibraryPath: configurationLibraryPath, Export: "bucket"}
	prior := registeredPlanningTarget(
		t,
		definition,
		registration,
		binding,
		registeredPlanningConfiguration(t, "https://old.example"),
		"logs",
		1,
		&registeredPlanningOutput{ID: "object-1", Value: "recorded"},
	)
	desired := registeredPlanningDesired(
		t,
		registration,
		binding,
		registeredPlanningConfiguration(t, "https://new.example"),
		"logs",
		1,
	)

	operation, err := runFixedPointPlanning(
		context.Background(),
		func(pass *planningPassState) (*ResourcePlanOperation, error) {
			return planRegisteredResourceOperation(
				context.Background(),
				pass,
				registeredResourcePlanningRequest{
					Address:              "resource.logs",
					Desired:              &desired,
					DesiredConfiguration: registeredPlanningConfig{Region: "new"},
					DesiredRegistration:  registration,
					Prior:                &prior,
					PriorConfiguration:   registeredPlanningConfig{Region: "old"},
					PriorRegistration:    registration,
				},
			)
		},
	)
	require.NoError(t, err)
	require.Equal(t, DecisionUpdate, operation.Decision)
	require.Equal(t, []string{"shared:old", "shared:new"}, capture.reads)
	want, err := encodeResourceOutputs(&registeredPlanningOutput{
		ID:    "object-1",
		Value: "observed-new",
	})
	require.NoError(t, err)
	require.Equal(t, want, *operation.Observation.Outputs)
}

func TestPlanRegisteredResourceOperationNormalizesMissingPrior(t *testing.T) {
	capture := &registeredPlanningCapture{
		read: func(
			string,
			registeredPlanningConfig,
			*registeredPlanningOutput,
		) (*registeredPlanningOutput, error) {
			return nil, ErrNotFound
		},
	}
	definition := registeredPlanningDefinition(1, IdentityConfiguration)
	registration := newRegisteredPlanningResource(t, definition, capture, "current")
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configuration := registrationConfigurationRecord(t, binding.LibraryPath)
	prior := registeredPlanningTarget(
		t,
		definition,
		registration,
		binding,
		configuration,
		"logs",
		1,
		&registeredPlanningOutput{ID: "object-1", Value: "recorded"},
	)
	desired := registeredPlanningDesired(
		t,
		registration,
		binding,
		configuration,
		"logs",
		1,
	)

	operation, err := runFixedPointPlanning(
		context.Background(),
		func(pass *planningPassState) (*ResourcePlanOperation, error) {
			return planRegisteredResourceOperation(
				context.Background(),
				pass,
				registeredResourcePlanningRequest{
					Address:              "resource.logs",
					Desired:              &desired,
					DesiredConfiguration: registeredPlanningConfig{Region: "current"},
					DesiredRegistration:  registration,
					Prior:                &prior,
					PriorConfiguration:   registeredPlanningConfig{Region: "current"},
					PriorRegistration:    registration,
				},
			)
		},
	)
	require.NoError(t, err)
	require.Equal(t, DecisionCreate, operation.Decision)
	require.Equal(t, []string{"remote-missing"}, operation.Reasons)
	require.Equal(t, ObservationAbsent, operation.Observation.Status)
	require.Equal(t, []string{"current:current"}, capture.reads)
}

func TestPlanRegisteredResourceOperationRequiresRegistrations(t *testing.T) {
	definition := registeredPlanningDefinition(1, IdentityConfiguration)
	capture := &registeredPlanningCapture{}
	registration := newRegisteredPlanningResource(t, definition, capture, "current")
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configuration := registrationConfigurationRecord(t, binding.LibraryPath)
	desired := registeredPlanningDesired(
		t,
		registration,
		binding,
		configuration,
		"logs",
		1,
	)
	prior := registeredPlanningTarget(
		t,
		definition,
		registration,
		binding,
		configuration,
		"logs",
		1,
		&registeredPlanningOutput{ID: "object-1"},
	)

	tests := []struct {
		name    string
		request registeredResourcePlanningRequest
		message string
	}{
		{
			name: "desired",
			request: registeredResourcePlanningRequest{
				Address: "resource.logs",
				Desired: &desired,
			},
			message: "desired resource registration is required",
		},
		{
			name: "prior",
			request: registeredResourcePlanningRequest{
				Address:            "resource.logs",
				Prior:              &prior,
				PriorConfiguration: registeredPlanningConfig{},
			},
			message: "prior resource registration is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := runFixedPointPlanning(
				context.Background(),
				func(pass *planningPassState) (*ResourcePlanOperation, error) {
					return planRegisteredResourceOperation(
						context.Background(),
						pass,
						test.request,
					)
				},
			)
			require.ErrorContains(t, err, test.message)
		})
	}
}
