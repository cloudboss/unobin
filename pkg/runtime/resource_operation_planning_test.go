package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type registeredPlanningCapture struct {
	reads []string
	read  func(string, *recordedConfiguration, *registeredPlanningOutput) (
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
	*recordedConfiguration,
) (*registeredPlanningOutput, error) {
	return &registeredPlanningOutput{ID: r.Name, Value: r.Name}, nil
}

func (r *registeredPlanningInput) Read(
	_ context.Context,
	config *recordedConfiguration,
	prior *registeredPlanningOutput,
) (*registeredPlanningOutput, error) {
	r.capture.reads = append(r.capture.reads, r.registration+":"+config.Endpoint)
	if r.capture.read != nil {
		return r.capture.read(r.registration, config, prior)
	}
	return prior, nil
}

func (r *registeredPlanningInput) Update(
	context.Context,
	*recordedConfiguration,
	Prior[registeredPlanningInput, *registeredPlanningOutput],
) (*registeredPlanningOutput, error) {
	return &registeredPlanningOutput{ID: r.Name, Value: r.Name}, nil
}

func (*registeredPlanningInput) Delete(
	context.Context,
	*recordedConfiguration,
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
	*recordedConfiguration,
] {
	name := InputField(func(value *registeredPlanningInput) *string {
		return &value.Name
	})
	return ResourceDefinition[
		registeredPlanningInput,
		*registeredPlanningOutput,
		*recordedConfiguration,
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
		*recordedConfiguration,
	],
	capture *registeredPlanningCapture,
	name string,
) *resourceDefinitionRegistration {
	t.Helper()
	registration, err := newResourceDefinitionRegistration[
		registeredPlanningInput,
		*registeredPlanningOutput,
		*recordedConfiguration,
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
		*recordedConfiguration,
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

func registeredOperationConfigurationDefinition(
	t *testing.T,
	libraryPath string,
	version int,
	migrate cfg.ConfigurationMigrationFunc,
) *resolvedConfigurationDefinition {
	t.Helper()
	definition, err := resolveConfigurationDefinition(
		libraryPath,
		configurationRegistration(version, migrate),
	)
	require.NoError(t, err)
	return &definition
}

func registeredOperationConfigurationRecord(
	t *testing.T,
	definition *resolvedConfigurationDefinition,
	endpoint string,
) ConfigurationRecord {
	t.Helper()
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

func registeredOperationConfiguration(
	t *testing.T,
	libraryPath string,
	endpoint string,
) (*resolvedConfigurationDefinition, ConfigurationRecord) {
	t.Helper()
	definition := registeredOperationConfigurationDefinition(t, libraryPath, 1, nil)
	return definition, registeredOperationConfigurationRecord(t, definition, endpoint)
}

func TestPlanRegisteredResourceStepsReevaluatesCompleteSet(t *testing.T) {
	definition := registeredPlanningDefinition(1, IdentityConfiguration)
	capture := &registeredPlanningCapture{}
	registration := newRegisteredPlanningResource(
		t,
		definition,
		capture,
		"current",
	)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
	prior := registeredPlanningTarget(
		t,
		definition,
		registration,
		binding,
		configuration,
		"logs",
		1,
		&registeredPlanningOutput{ID: "logs-1", Value: "logs"},
	)
	existing := registeredPlanningDesired(
		t,
		registration,
		binding,
		configuration,
		"logs",
		1,
	)
	created := registeredPlanningDesired(
		t,
		registration,
		binding,
		configuration,
		"archive",
		1,
	)
	evaluations := 0

	steps, err := planRegisteredResourceSteps(
		context.Background(),
		func(*planningPassState) ([]registeredResourcePlanningRequest, error) {
			evaluations++
			return []registeredResourcePlanningRequest{
				{
					Address:             "resource.logs",
					DependsOn:           []string{},
					Desired:             &existing,
					DesiredConfigType:   configurationDefinition,
					DesiredRegistration: registration,
					Prior:               &prior,
					PriorConfigType:     configurationDefinition,
					PriorRegistration:   registration,
				},
				{
					Address:             "resource.archive",
					DependsOn:           []string{"resource.logs"},
					Desired:             &created,
					DesiredConfigType:   configurationDefinition,
					DesiredRegistration: registration,
				},
				{Address: "resource.omitted"},
			}, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 2, evaluations)
	require.Len(t, capture.reads, 1)
	require.Equal(t, []string{"current:current"}, capture.reads)
	require.Len(t, steps, 2)
	require.Equal(t, "resource.logs", steps[0].Address)
	require.Equal(t, DecisionNoOp, steps[0].Operation.Resource.Decision)
	require.Equal(t, "resource.archive", steps[1].Address)
	require.Equal(t, DecisionCreate, steps[1].Operation.Resource.Decision)
	require.Equal(t, []string{"resource.logs"}, steps[1].DependsOn)
}

func TestPlanRegisteredResourceStepsReturnsRequiredEmptySet(t *testing.T) {
	steps, err := planRegisteredResourceSteps(
		context.Background(),
		func(*planningPassState) ([]registeredResourcePlanningRequest, error) {
			return nil, nil
		},
	)
	require.NoError(t, err)
	require.NotNil(t, steps)
	require.Empty(t, steps)
}

func TestPlanRegisteredResourceStepsRejectsDuplicateAddresses(t *testing.T) {
	_, err := planRegisteredResourceSteps(
		context.Background(),
		func(*planningPassState) ([]registeredResourcePlanningRequest, error) {
			return []registeredResourcePlanningRequest{
				{Address: "resource.logs"},
				{Address: "resource.logs"},
			}, nil
		},
	)
	require.ErrorContains(t, err, `duplicate resource address "resource.logs"`)
}

func TestPlanRegisteredResourceStepsRequiresEvaluator(t *testing.T) {
	steps, err := planRegisteredResourceSteps(context.Background(), nil)
	require.ErrorContains(t, err, "resource planning evaluator is required")
	require.Nil(t, steps)
}

func TestPlanRegisteredResourceStepBuildsCompleteStep(t *testing.T) {
	definition := registeredPlanningDefinition(1, IdentityConfiguration)
	registration := newRegisteredPlanningResource(
		t,
		definition,
		&registeredPlanningCapture{},
		"current",
	)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
	desired := registeredPlanningDesired(
		t,
		registration,
		binding,
		configuration,
		"logs",
		1,
	)
	dependencies := []string{"resource.network"}

	step, err := runFixedPointPlanning(
		context.Background(),
		func(pass *planningPassState) (*PlanStepV2, error) {
			return planRegisteredResourceStep(
				context.Background(),
				pass,
				registeredResourcePlanningRequest{
					Address:             "resource.logs",
					DependsOn:           dependencies,
					Desired:             &desired,
					DesiredConfigType:   configurationDefinition,
					DesiredRegistration: registration,
				},
			)
		},
	)
	require.NoError(t, err)
	require.NotNil(t, step)
	require.Equal(t, "resource.logs", step.Address)
	require.Equal(t, NodeResource, step.Kind)
	require.Equal(t, []string{"resource.network"}, step.DependsOn)
	require.Equal(t, StepResource, step.Operation.Kind)
	require.Equal(t, DecisionCreate, step.Operation.Resource.Decision)
	require.Equal(t, desired, *step.Operation.Resource.Desired)

	dependencies[0] = "resource.changed"
	require.Equal(t, []string{"resource.network"}, step.DependsOn)
}

func TestPlanRegisteredResourceStepRejectsInvalidDependencies(t *testing.T) {
	definition := registeredPlanningDefinition(1, IdentityConfiguration)
	registration := newRegisteredPlanningResource(
		t,
		definition,
		&registeredPlanningCapture{},
		"current",
	)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
	desired := registeredPlanningDesired(
		t,
		registration,
		binding,
		configuration,
		"logs",
		1,
	)

	_, err := runFixedPointPlanning(
		context.Background(),
		func(pass *planningPassState) (*PlanStepV2, error) {
			return planRegisteredResourceStep(
				context.Background(),
				pass,
				registeredResourcePlanningRequest{
					Address:             "resource.logs",
					DependsOn:           []string{"resource.z", "resource.a"},
					Desired:             &desired,
					DesiredConfigType:   configurationDefinition,
					DesiredRegistration: registration,
				},
			)
		},
	)
	require.ErrorContains(t, err, "dependencies must be unique and sorted")
}

func TestPlanRegisteredResourceStepOmitsAbsentResource(t *testing.T) {
	step, err := runFixedPointPlanning(
		context.Background(),
		func(pass *planningPassState) (*PlanStepV2, error) {
			return planRegisteredResourceStep(
				context.Background(),
				pass,
				registeredResourcePlanningRequest{Address: "resource.logs"},
			)
		},
	)
	require.NoError(t, err)
	require.Nil(t, step)
}

func TestPrepareRegisteredResourceDesiredTarget(t *testing.T) {
	definition := registeredPlanningDefinition(1, IdentityConfiguration)
	registration := newRegisteredPlanningResource(
		t,
		definition,
		&registeredPlanningCapture{},
		"current",
	)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
	desired := registeredPlanningDesired(
		t,
		registration,
		binding,
		configuration,
		"logs",
		1,
	)

	prepared, decoded, err := prepareRegisteredResourceDesiredTarget(
		&desired,
		configurationDefinition,
	)
	require.NoError(t, err)
	require.NotNil(t, prepared)
	require.NotSame(t, &desired, prepared)
	require.True(t, sameConfigurationRecord(configuration, *prepared.Configuration.Record))
	decodedConfiguration, ok := decoded.(*recordedConfiguration)
	require.True(t, ok)
	require.Equal(t, "current", decodedConfiguration.Endpoint)

	pending := desired
	pending.Configuration = PlannedConfiguration{
		Kind:        PlannedConfigurationPending,
		PendingRefs: []string{"resource.region.id"},
	}
	prepared, decoded, err = prepareRegisteredResourceDesiredTarget(
		&pending,
		configurationDefinition,
	)
	require.NoError(t, err)
	require.NotNil(t, prepared)
	require.Equal(t, pending.Configuration, prepared.Configuration)
	require.Nil(t, decoded)

	otherDefinition, _ := registeredOperationConfiguration(
		t,
		"example.com/other",
		"other",
	)
	_, _, err = prepareRegisteredResourceDesiredTarget(&pending, otherDefinition)
	require.ErrorContains(
		t,
		err,
		"desired configuration definition does not match binding",
	)

	prepared, decoded, err = prepareRegisteredResourceDesiredTarget(nil, nil)
	require.NoError(t, err)
	require.Nil(t, prepared)
	require.Nil(t, decoded)

	_, _, err = prepareRegisteredResourceDesiredTarget(&desired, nil)
	require.ErrorContains(t, err, "desired configuration definition is required")
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
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
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
					DesiredConfigType:   configurationDefinition,
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
					Address:           "resource.logs",
					Prior:             &prior,
					PriorConfigType:   configurationDefinition,
					PriorRegistration: registration,
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
	priorConfigurationDefinition, priorConfiguration := registeredOperationConfiguration(
		t,
		priorBinding.LibraryPath,
		"old",
	)
	desiredConfigurationDefinition, desiredConfiguration := registeredOperationConfiguration(
		t,
		desiredBinding.LibraryPath,
		"new",
	)
	prior := registeredPlanningTarget(
		t,
		priorDefinition,
		priorRegistration,
		priorBinding,
		priorConfiguration,
		"old",
		1,
		&registeredPlanningOutput{ID: "object-1", Value: "recorded"},
	)
	desired := registeredPlanningDesired(
		t,
		desiredRegistration,
		desiredBinding,
		desiredConfiguration,
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
					Address:             "resource.logs",
					Desired:             &desired,
					DesiredConfigType:   desiredConfigurationDefinition,
					DesiredRegistration: desiredRegistration,
					Prior:               &prior,
					PriorConfigType:     priorConfigurationDefinition,
					PriorRegistration:   priorRegistration,
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
		config *recordedConfiguration,
		_ *registeredPlanningOutput,
	) (*registeredPlanningOutput, error) {
		return &registeredPlanningOutput{
			ID:    "object-1",
			Value: "observed-" + config.Endpoint,
		}, nil
	}
	definition := registeredPlanningDefinition(1, IdentityGlobal)
	registration := newRegisteredPlanningResource(t, definition, capture, "shared")
	binding := Binding{LibraryPath: configurationLibraryPath, Export: "bucket"}
	configurationDefinition := registeredOperationConfigurationDefinition(
		t,
		binding.LibraryPath,
		1,
		nil,
	)
	prior := registeredPlanningTarget(
		t,
		definition,
		registration,
		binding,
		registeredOperationConfigurationRecord(t, configurationDefinition, "old"),
		"logs",
		1,
		&registeredPlanningOutput{ID: "object-1", Value: "recorded"},
	)
	desired := registeredPlanningDesired(
		t,
		registration,
		binding,
		registeredOperationConfigurationRecord(t, configurationDefinition, "new"),
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
					Address:             "resource.logs",
					Desired:             &desired,
					DesiredConfigType:   configurationDefinition,
					DesiredRegistration: registration,
					Prior:               &prior,
					PriorConfigType:     configurationDefinition,
					PriorRegistration:   registration,
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
			*recordedConfiguration,
			*registeredPlanningOutput,
		) (*registeredPlanningOutput, error) {
			return nil, ErrNotFound
		},
	}
	definition := registeredPlanningDefinition(1, IdentityConfiguration)
	registration := newRegisteredPlanningResource(t, definition, capture, "current")
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
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
					Address:             "resource.logs",
					Desired:             &desired,
					DesiredConfigType:   configurationDefinition,
					DesiredRegistration: registration,
					Prior:               &prior,
					PriorConfigType:     configurationDefinition,
					PriorRegistration:   registration,
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
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
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
				Address:         "resource.logs",
				Prior:           &prior,
				PriorConfigType: configurationDefinition,
			},
			message: "prior resource registration is required",
		},
		{
			name: "desired configuration",
			request: registeredResourcePlanningRequest{
				Address:             "resource.logs",
				Desired:             &desired,
				DesiredRegistration: registration,
			},
			message: "desired configuration definition is required",
		},
		{
			name: "prior configuration",
			request: registeredResourcePlanningRequest{
				Address:           "resource.logs",
				Prior:             &prior,
				PriorRegistration: registration,
			},
			message: "prior configuration definition is required",
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
