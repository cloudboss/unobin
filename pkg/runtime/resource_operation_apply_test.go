package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type registeredApplyCapture struct {
	calls          []string
	createResult   *registeredApplyOutput
	readResult     *registeredApplyOutput
	readErr        error
	readInput      registeredApplyInput
	readPrior      *registeredApplyOutput
	deletedInput   registeredApplyInput
	deletedOutput  *registeredApplyOutput
	migrationCalls int
}

type registeredApplyInput struct {
	Name string `ub:"name"`
	Size int    `ub:"size"`

	capture *registeredApplyCapture
}

type registeredApplyOutput struct {
	ID    string `ub:"id"`
	Value string `ub:"value"`
}

func (r *registeredApplyInput) Create(
	_ context.Context,
	config *recordedConfiguration,
) (*registeredApplyOutput, error) {
	r.capture.calls = append(r.capture.calls, "create:"+config.Endpoint)
	return r.capture.createResult, nil
}

func (r *registeredApplyInput) Read(
	_ context.Context,
	config *recordedConfiguration,
	prior *registeredApplyOutput,
) (*registeredApplyOutput, error) {
	r.capture.calls = append(r.capture.calls, "read:"+config.Endpoint)
	r.capture.readInput = *r
	r.capture.readPrior = prior
	if r.capture.readErr != nil {
		return nil, r.capture.readErr
	}
	return r.capture.readResult, nil
}

func (r *registeredApplyInput) Update(
	_ context.Context,
	config *recordedConfiguration,
	_ Prior[registeredApplyInput, *registeredApplyOutput],
) (*registeredApplyOutput, error) {
	r.capture.calls = append(r.capture.calls, "update:"+config.Endpoint)
	return r.capture.createResult, nil
}

func (r *registeredApplyInput) Delete(
	_ context.Context,
	config *recordedConfiguration,
	outputs *registeredApplyOutput,
) error {
	r.capture.calls = append(r.capture.calls, "delete:"+config.Endpoint)
	r.capture.deletedInput = *r
	r.capture.deletedOutput = outputs
	return nil
}

func registeredApplyDefinition(
	schemaVersion int,
	migrate ResourceMigrationFunc,
) ResourceDefinition[
	registeredApplyInput,
	*registeredApplyOutput,
	*recordedConfiguration,
] {
	name := InputField(func(value *registeredApplyInput) *string {
		return &value.Name
	})
	return ResourceDefinition[
		registeredApplyInput,
		*registeredApplyOutput,
		*recordedConfiguration,
	]{
		SchemaVersion: schemaVersion,
		Migrate:       migrate,
		Identity: ResourceIdentity[registeredApplyInput, *registeredApplyOutput]{
			Version:       1,
			Scope:         IdentityConfiguration,
			AddressInputs: []AnyInputField[registeredApplyInput]{name},
			StableID: func(
				_ registeredApplyInput,
				outputs *registeredApplyOutput,
			) (string, error) {
				return outputs.ID, nil
			},
		},
	}
}

func newRegisteredApplyResource(
	t *testing.T,
	definition ResourceDefinition[
		registeredApplyInput,
		*registeredApplyOutput,
		*recordedConfiguration,
	],
	capture *registeredApplyCapture,
) *resourceDefinitionRegistration {
	t.Helper()
	registration, err := newResourceDefinitionRegistration[
		registeredApplyInput,
		*registeredApplyOutput,
		*recordedConfiguration,
		*registeredApplyInput,
	](definition, func() *registeredApplyInput {
		return &registeredApplyInput{capture: capture}
	})
	require.NoError(t, err)
	return registration
}

func registeredApplyTarget(
	t *testing.T,
	definition ResourceDefinition[
		registeredApplyInput,
		*registeredApplyOutput,
		*recordedConfiguration,
	],
	registration *resourceDefinitionRegistration,
	binding Binding,
	configuration ConfigurationRecord,
	schemaVersion int,
	inputs registeredApplyInput,
	outputs *registeredApplyOutput,
) ResourceTarget {
	t.Helper()
	encodedInputs, err := registration.prepareInputs(map[string]any{
		"name": inputs.Name,
		"size": inputs.Size,
	})
	require.NoError(t, err)
	encodedOutputs, err := encodeResourceOutputs(outputs)
	require.NoError(t, err)
	resolved, err := resolveResourceDefinition(definition)
	require.NoError(t, err)
	identity, err := resolved.newIdentityRecord(
		binding.LibraryPath,
		binding.Export,
		inputs,
		outputs,
	)
	require.NoError(t, err)
	target := ResourceTarget{
		Binding:              binding,
		SchemaVersion:        schemaVersion,
		Inputs:               encodedInputs,
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

func registeredApplyDesired(
	t *testing.T,
	registration *resourceDefinitionRegistration,
	binding Binding,
	configuration ConfigurationRecord,
	inputs registeredApplyInput,
) PlannedResourceTarget {
	t.Helper()
	encodedInputs, err := registration.prepareInputs(map[string]any{
		"name": inputs.Name,
		"size": inputs.Size,
	})
	require.NoError(t, err)
	desired := PlannedResourceTarget{
		Binding: binding,
		Inputs:  encodedInputs,
		Configuration: PlannedConfiguration{
			Kind:   PlannedConfigurationConcrete,
			Record: &configuration,
		},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	require.NoError(t, desired.Validate())
	return desired
}

func TestApplyRegisteredResourceOperationUsesRecordedRegistrationForReplacement(
	t *testing.T,
) {
	capture := &registeredApplyCapture{
		createResult: &registeredApplyOutput{ID: "object-2", Value: "created"},
		readResult:   &registeredApplyOutput{ID: "object-1", Value: "fresh"},
	}
	definition := registeredApplyDefinition(1, nil)
	priorRegistration := newRegisteredApplyResource(t, definition, capture)
	desiredRegistration := newRegisteredApplyResource(t, definition, capture)
	priorBinding := Binding{LibraryPath: "example.com/prior", Export: "bucket"}
	desiredBinding := Binding{LibraryPath: "example.com/current", Export: "archive"}
	priorConfigurationDefinition, priorConfiguration := registeredOperationConfiguration(
		t,
		priorBinding.LibraryPath,
		"prior",
	)
	_, desiredConfiguration := registeredOperationConfiguration(
		t,
		desiredBinding.LibraryPath,
		"desired",
	)
	prior := registeredApplyTarget(
		t,
		definition,
		priorRegistration,
		priorBinding,
		priorConfiguration,
		1,
		registeredApplyInput{Name: "logs", Size: 1},
		&registeredApplyOutput{ID: "object-1", Value: "recorded"},
	)
	desired := registeredApplyDesired(
		t,
		desiredRegistration,
		desiredBinding,
		desiredConfiguration,
		registeredApplyInput{Name: "archive", Size: 2},
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
					DesiredConfiguration: &recordedConfiguration{Endpoint: "desired"},
					DesiredRegistration:  desiredRegistration,
					Prior:                &prior,
					PriorConfigType:      priorConfigurationDefinition,
					PriorRegistration:    priorRegistration,
				},
			)
		},
	)
	require.NoError(t, err)
	require.Equal(t, DecisionReplace, operation.Decision)
	capture.calls = nil

	var persisted *ResourceTarget
	target, err := applyRegisteredResourceOperation(
		context.Background(),
		registeredResourceApplyOperationRequest{
			Address:              "resource.logs",
			Operation:            *operation,
			Desired:              &desired,
			DesiredConfiguration: &recordedConfiguration{Endpoint: "desired"},
			DesiredRegistration:  desiredRegistration,
			Prior:                &prior,
			PriorConfigType:      priorConfigurationDefinition,
			PriorRegistration:    priorRegistration,
			Observation:          operation.Observation,
			DependsOn:            []string{"resource.network"},
			Persist: func(_ context.Context, target *ResourceTarget) error {
				capture.calls = append(capture.calls, "persist")
				persisted = target
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"read:prior", "delete:prior", "create:desired", "persist"},
		capture.calls,
	)
	require.Equal(t, "logs", capture.deletedInput.Name)
	require.Equal(t, 1, capture.deletedInput.Size)
	require.Equal(t, capture.readResult, capture.deletedOutput)
	require.Equal(t, target, persisted)
	require.Equal(t, desiredBinding, target.Binding)
	require.Equal(t, []string{"resource.network"}, target.DependsOn)
	require.Equal(t, "object-2", stringValue(target.Identity.StableID))
}

func TestApplyRegisteredResourceOperationMigratesRecordedConfiguration(t *testing.T) {
	capture := &registeredApplyCapture{
		readResult: &registeredApplyOutput{ID: "object-1", Value: "fresh"},
	}
	definition := registeredApplyDefinition(1, nil)
	registration := newRegisteredApplyResource(t, definition, capture)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	_, oldConfiguration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"old",
	)
	prior := registeredApplyTarget(
		t,
		definition,
		registration,
		binding,
		oldConfiguration,
		1,
		registeredApplyInput{Name: "logs", Size: 1},
		&registeredApplyOutput{ID: "object-1", Value: "recorded"},
	)
	configurationMigrationCalls := 0
	configurationDefinition := registeredOperationConfigurationDefinition(
		t,
		binding.LibraryPath,
		2,
		func(oldVersion int, value EncodedValue) (EncodedValue, error) {
			configurationMigrationCalls++
			require.Equal(t, 1, oldVersion)
			fields, ok := value.ObjectFields()
			require.True(t, ok)
			fields["endpoint"] = StringValue("migrated")
			return ObjectValue(fields)
		},
	)

	operation, err := runFixedPointPlanning(
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
	require.Equal(t, 1, configurationMigrationCalls)
	require.Equal(t, 2, operation.Prior.Configuration.SchemaVersion)
	require.Equal(t, []string{"read:migrated"}, capture.calls)

	configurationMigrationCalls = 0
	capture.calls = nil
	removed := false
	target, err := applyRegisteredResourceOperation(
		context.Background(),
		registeredResourceApplyOperationRequest{
			Address:           "resource.logs",
			Operation:         *operation,
			Prior:             &prior,
			PriorConfigType:   configurationDefinition,
			PriorRegistration: registration,
			Observation:       operation.Observation,
			DependsOn:         []string{},
			Persist: func(_ context.Context, target *ResourceTarget) error {
				capture.calls = append(capture.calls, "persist")
				removed = target == nil
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Nil(t, target)
	require.True(t, removed)
	require.Equal(t, 1, configurationMigrationCalls)
	require.Equal(
		t,
		[]string{"read:migrated", "delete:migrated", "persist"},
		capture.calls,
	)
}

func TestApplyRegisteredResourceOperationMigratesPriorBeforeDestroy(t *testing.T) {
	capture := &registeredApplyCapture{
		readResult: &registeredApplyOutput{ID: "object-1", Value: "fresh"},
	}
	migratedInputs, _, err := prepareResourceInputs[registeredApplyInput](map[string]any{
		"name": "logs",
		"size": 2,
	})
	require.NoError(t, err)
	migratedOutputs, err := encodeResourceOutputs(
		&registeredApplyOutput{ID: "object-1", Value: "migrated"},
	)
	require.NoError(t, err)
	definition := registeredApplyDefinition(
		2,
		func(
			oldVersion int,
			_ ResourceMigrationState,
		) (ResourceMigrationState, error) {
			capture.migrationCalls++
			if oldVersion != 1 {
				return ResourceMigrationState{}, fmt.Errorf(
					"unexpected resource version %d",
					oldVersion,
				)
			}
			return ResourceMigrationState{
				Inputs:  migratedInputs,
				Outputs: migratedOutputs,
			}, nil
		},
	)
	registration := newRegisteredApplyResource(t, definition, capture)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"prior",
	)
	prior := registeredApplyTarget(
		t,
		definition,
		registration,
		binding,
		configuration,
		1,
		registeredApplyInput{Name: "logs", Size: 1},
		&registeredApplyOutput{ID: "object-1", Value: "recorded"},
	)
	operation, err := runFixedPointPlanning(
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
	capture.calls = nil
	capture.migrationCalls = 0
	capture.readInput = registeredApplyInput{}
	capture.readPrior = nil

	removed := false
	target, err := applyRegisteredResourceOperation(
		context.Background(),
		registeredResourceApplyOperationRequest{
			Address:           "resource.logs",
			Operation:         *operation,
			Prior:             &prior,
			PriorConfigType:   configurationDefinition,
			PriorRegistration: registration,
			Observation:       operation.Observation,
			DependsOn:         []string{},
			Persist: func(_ context.Context, target *ResourceTarget) error {
				capture.calls = append(capture.calls, "persist")
				removed = target == nil
				return nil
			},
		},
	)
	require.NoError(t, err)
	require.Nil(t, target)
	require.True(t, removed)
	require.Equal(t, 1, capture.migrationCalls)
	require.Equal(t, []string{"read:prior", "delete:prior", "persist"}, capture.calls)
	require.Equal(t, 2, capture.readInput.Size)
	require.Equal(t, "migrated", capture.readPrior.Value)
	require.Equal(t, 2, capture.deletedInput.Size)
	require.Equal(t, "fresh", capture.deletedOutput.Value)
}

func TestApplyRegisteredResourceOperationRequiresRegistrations(t *testing.T) {
	capture := &registeredApplyCapture{
		createResult: &registeredApplyOutput{ID: "object-1"},
		readResult:   &registeredApplyOutput{ID: "object-1"},
	}
	definition := registeredApplyDefinition(1, nil)
	registration := newRegisteredApplyResource(t, definition, capture)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"prior",
	)
	desired := registeredApplyDesired(
		t,
		registration,
		binding,
		configuration,
		registeredApplyInput{Name: "logs", Size: 1},
	)
	create, err := registration.planResourceOperation(resourcePlanningRequest{Desired: &desired})
	require.NoError(t, err)

	_, err = applyRegisteredResourceOperation(
		context.Background(),
		registeredResourceApplyOperationRequest{
			Address:   "resource.logs",
			Operation: *create,
			Desired:   &desired,
		},
	)
	require.ErrorContains(t, err, "desired resource registration is required")

	prior := registeredApplyTarget(
		t,
		definition,
		registration,
		binding,
		configuration,
		1,
		registeredApplyInput{Name: "logs", Size: 1},
		&registeredApplyOutput{ID: "object-1"},
	)
	operation, err := runFixedPointPlanning(
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
	capture.calls = nil

	_, err = applyRegisteredResourceOperation(
		context.Background(),
		registeredResourceApplyOperationRequest{
			Address:           "resource.logs",
			Operation:         *operation,
			Prior:             &prior,
			PriorRegistration: registration,
			Observation:       operation.Observation,
		},
	)
	require.ErrorContains(t, err, "prior configuration definition is required")
	require.Empty(t, capture.calls)

	_, err = applyRegisteredResourceOperation(
		context.Background(),
		registeredResourceApplyOperationRequest{
			Address:         "resource.logs",
			Operation:       *operation,
			Prior:           &prior,
			PriorConfigType: configurationDefinition,
			Observation:     operation.Observation,
		},
	)
	require.ErrorContains(t, err, "prior resource registration is required")
	require.Empty(t, capture.calls)
}
