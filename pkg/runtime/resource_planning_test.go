package runtime

import (
	"maps"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type resourcePlanningFixture struct {
	definition resolvedResourceDefinition[ruleInput, *ruleOutput, NoConfig]
	request    resourcePlanningRequest[ruleInput, *ruleOutput]
}

func newResourcePlanningFixture(
	t *testing.T,
	definition ruleDefinition,
) resourcePlanningFixture {
	t.Helper()
	resolved, err := resolveResourceDefinition(definition)
	require.NoError(t, err)

	binding := Binding{LibraryPath: configurationLibraryPath, Export: "bucket"}
	inputs := ruleInput{
		Name:     "logs",
		Region:   "us-east-1",
		Labels:   map[string]string{"env": "test"},
		Settings: &ruleSettings{Tier: "standard", Replicas: 2},
		Zones:    []string{"a", "b"},
	}
	outputs := &ruleOutput{
		ID:     "bucket-1",
		ETag:   "etag-1",
		Status: &ruleStatus{Code: "ready"},
	}
	encodedInputs := encodeRuleInput(t, inputs, nil)
	encodedOutputs := encodeRuleOutput(t, outputs)
	configuration := planningConfigurationRecord(t, configurationLibraryPath, "https://api.example")
	identity, err := resolved.newIdentityRecord(binding.LibraryPath, binding.Export, inputs, outputs)
	require.NoError(t, err)
	prior := ResourceTarget{
		Binding:              binding,
		SchemaVersion:        definition.SchemaVersion,
		Inputs:               encodedInputs,
		Outputs:              encodedOutputs,
		Configuration:        configuration,
		Identity:             identity,
		DependsOn:            []string{},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	desired := PlannedResourceTarget{
		Binding: binding,
		Inputs:  encodedInputs,
		Configuration: PlannedConfiguration{
			Kind:   PlannedConfigurationConcrete,
			Record: new(configuration),
		},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	observation := ResourceObservation{
		Status:   ObservationPresent,
		Outputs:  new(encodedOutputs),
		Identity: new(identity),
	}
	return resourcePlanningFixture{
		definition: resolved,
		request: resourcePlanningRequest[ruleInput, *ruleOutput]{
			Desired: &preparedResourceDesired[ruleInput]{
				Target: desired,
				Inputs: inputs,
			},
			Prior: &preparedResourcePrior[ruleInput, *ruleOutput]{
				Target:  prior,
				Inputs:  inputs,
				Outputs: outputs,
			},
			RecordedObservation: &preparedResourceObservation[*ruleOutput]{
				Observation: observation,
				Outputs:     outputs,
			},
		},
	}
}

func encodeRuleInput(
	t *testing.T,
	input ruleInput,
	overrides map[string]EncodedValue,
) EncodedValue {
	t.Helper()
	labels := make(map[string]EncodedValue, len(input.Labels))
	for key, value := range input.Labels {
		labels[key] = StringValue(value)
	}
	encodedLabels, err := MapValue(labels)
	require.NoError(t, err)
	zones := make([]EncodedValue, len(input.Zones))
	for i, zone := range input.Zones {
		zones[i] = StringValue(zone)
	}
	encodedZones, err := ListValue(zones)
	require.NoError(t, err)
	settings := NullValue()
	if input.Settings != nil {
		settings, err = ObjectValue(map[string]EncodedValue{
			"replicas": IntegerValue(int64(input.Settings.Replicas)),
			"tier":     StringValue(input.Settings.Tier),
		})
		require.NoError(t, err)
	}
	fields := map[string]EncodedValue{
		"labels":   encodedLabels,
		"name":     StringValue(input.Name),
		"region":   StringValue(input.Region),
		"settings": settings,
		"zones":    encodedZones,
	}
	maps.Copy(fields, overrides)
	value, err := ObjectValue(fields)
	require.NoError(t, err)
	return value
}

func encodeRuleOutput(t *testing.T, output *ruleOutput) EncodedValue {
	t.Helper()
	status := NullValue()
	var err error
	if output.Status != nil {
		status, err = ObjectValue(map[string]EncodedValue{
			"code": StringValue(output.Status.Code),
		})
		require.NoError(t, err)
	}
	value, err := ObjectValue(map[string]EncodedValue{
		"etag":   StringValue(output.ETag),
		"id":     StringValue(output.ID),
		"status": status,
	})
	require.NoError(t, err)
	return value
}

func planningConfigurationRecord(
	t *testing.T,
	libraryPath string,
	endpoint string,
) ConfigurationRecord {
	t.Helper()
	definition, err := resolveConfigurationDefinition(
		libraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	fields, ok := testConfigurationValue(t).ObjectFields()
	require.True(t, ok)
	fields["endpoint"] = StringValue(endpoint)
	value, err := ObjectValue(fields)
	require.NoError(t, err)
	record, err := definition.newConfigurationRecordWith(
		"library-config.cloud",
		value,
		[]string{"/servers/1", "/credentials", "/nullable", "/labels/a~1b~0"},
		nil,
		deterministicIDs(
			strings.Repeat("1", 32),
			strings.Repeat("2", 32),
			strings.Repeat("3", 32),
			strings.Repeat("4", 32),
		),
	)
	require.NoError(t, err)
	return record
}

func setPlanningDesiredInputs(
	t *testing.T,
	fixture *resourcePlanningFixture,
	inputs ruleInput,
	overrides map[string]EncodedValue,
) {
	t.Helper()
	fixture.request.Desired.Inputs = inputs
	fixture.request.Desired.Target.Inputs = encodeRuleInput(t, inputs, overrides)
}

func setPlanningObservation(
	t *testing.T,
	fixture *resourcePlanningFixture,
	observation *preparedResourceObservation[*ruleOutput],
	outputs *ruleOutput,
) {
	t.Helper()
	encoded := encodeRuleOutput(t, outputs)
	identity, err := fixture.definition.newIdentityRecord(
		fixture.request.Prior.Target.Binding.LibraryPath,
		fixture.request.Prior.Target.Binding.Export,
		fixture.request.Prior.Inputs,
		outputs,
	)
	require.NoError(t, err)
	observation.Outputs = outputs
	observation.Observation = ResourceObservation{
		Status:   ObservationPresent,
		Outputs:  new(encoded),
		Identity: new(identity),
	}
}

func planFixtureOperation(
	t *testing.T,
	fixture resourcePlanningFixture,
) *ResourcePlanOperation {
	t.Helper()
	operation, err := fixture.definition.planResourceOperation(fixture.request)
	require.NoError(t, err)
	if operation != nil {
		require.NoError(t, operation.Validate())
	}
	return operation
}

func TestResourcePlanningHandlesPresenceMatrix(t *testing.T) {
	t.Run("no desired or prior", func(t *testing.T) {
		fixture := newResourcePlanningFixture(t, validRuleDefinition())
		fixture.request.Desired = nil
		fixture.request.Prior = nil
		fixture.request.RecordedObservation = nil
		require.Nil(t, planFixtureOperation(t, fixture))
	})

	t.Run("initial create", func(t *testing.T) {
		fixture := newResourcePlanningFixture(t, validRuleDefinition())
		fixture.request.Prior = nil
		fixture.request.RecordedObservation = nil
		operation := planFixtureOperation(t, fixture)
		require.Equal(t, DecisionCreate, operation.Decision)
		require.Empty(t, operation.Reasons)
	})

	t.Run("remote missing create", func(t *testing.T) {
		fixture := newResourcePlanningFixture(t, validRuleDefinition())
		fixture.request.RecordedObservation = &preparedResourceObservation[*ruleOutput]{
			Observation: ResourceObservation{Status: ObservationAbsent},
		}
		operation := planFixtureOperation(t, fixture)
		require.Equal(t, DecisionCreate, operation.Decision)
		require.Equal(t, []string{"remote-missing"}, operation.Reasons)
	})

	for _, status := range []ObservationStatus{ObservationPresent, ObservationAbsent} {
		t.Run("destroy "+string(status), func(t *testing.T) {
			fixture := newResourcePlanningFixture(t, validRuleDefinition())
			fixture.request.Desired = nil
			if status == ObservationAbsent {
				fixture.request.RecordedObservation = &preparedResourceObservation[*ruleOutput]{
					Observation: ResourceObservation{Status: ObservationAbsent},
				}
			}
			operation := planFixtureOperation(t, fixture)
			require.Equal(t, DecisionDestroy, operation.Decision)
			require.Equal(t, status, operation.Observation.Status)
		})
	}
}

func TestResourcePlanningSelectsCompleteDecisionTable(t *testing.T) {
	tests := []struct {
		name     string
		change   func(*testing.T, *resourcePlanningFixture)
		decision Decision
		reasons  []string
	}{
		{
			name: "binding",
			change: func(t *testing.T, fixture *resourcePlanningFixture) {
				fixture.request.Desired.Target.Binding.Export = "archive"
			},
			decision: DecisionReplace,
			reasons:  []string{"binding"},
		},
		{
			name: "pending configuration",
			change: func(t *testing.T, fixture *resourcePlanningFixture) {
				fixture.request.Desired.Target.Configuration = pendingOperationConfiguration()
			},
			decision: DecisionReplace,
			reasons:  []string{"configuration-pending"},
		},
		{
			name: "binding with pending configuration",
			change: func(t *testing.T, fixture *resourcePlanningFixture) {
				fixture.request.Desired.Target.Binding.Export = "archive"
				fixture.request.Desired.Target.Configuration = pendingOperationConfiguration()
			},
			decision: DecisionReplace,
			reasons:  []string{"binding", "configuration-pending"},
		},
		{
			name: "configuration scoped change",
			change: func(t *testing.T, fixture *resourcePlanningFixture) {
				record := planningConfigurationRecord(
					t,
					configurationLibraryPath,
					"https://other.example",
				)
				fixture.request.Desired.Target.Configuration.Record = new(record)
			},
			decision: DecisionReplace,
			reasons:  []string{"configuration"},
		},
		{
			name: "address input",
			change: func(t *testing.T, fixture *resourcePlanningFixture) {
				inputs := fixture.request.Desired.Inputs
				inputs.Name = "archive"
				setPlanningDesiredInputs(t, fixture, inputs, nil)
			},
			decision: DecisionReplace,
			reasons:  []string{"address:name"},
		},
		{
			name: "input replacement",
			change: func(t *testing.T, fixture *resourcePlanningFixture) {
				inputs := fixture.request.Desired.Inputs
				inputs.Region = "us-west-2"
				setPlanningDesiredInputs(t, fixture, inputs, nil)
			},
			decision: DecisionReplace,
			reasons:  []string{"input:region"},
		},
		{
			name: "conditional input replacement",
			change: func(t *testing.T, fixture *resourcePlanningFixture) {
				inputs := fixture.request.Desired.Inputs
				settings := *inputs.Settings
				settings.Replicas = 3
				inputs.Settings = &settings
				setPlanningDesiredInputs(t, fixture, inputs, nil)
			},
			decision: DecisionReplace,
			reasons:  []string{"input:settings.replicas"},
		},
		{
			name: "conditional input update",
			change: func(t *testing.T, fixture *resourcePlanningFixture) {
				inputs := fixture.request.Desired.Inputs
				settings := *inputs.Settings
				settings.Replicas = 1
				inputs.Settings = &settings
				setPlanningDesiredInputs(t, fixture, inputs, nil)
			},
			decision: DecisionUpdate,
			reasons:  []string{},
		},
		{
			name: "drift replacement",
			change: func(t *testing.T, fixture *resourcePlanningFixture) {
				outputs := *fixture.request.RecordedObservation.Outputs
				outputs.ETag = "etag-2"
				setPlanningObservation(t, fixture, fixture.request.RecordedObservation, &outputs)
			},
			decision: DecisionReplace,
			reasons:  []string{"drift:etag"},
		},
		{
			name: "ordinary input update",
			change: func(t *testing.T, fixture *resourcePlanningFixture) {
				inputs := fixture.request.Desired.Inputs
				inputs.Zones = []string{"a", "c"}
				setPlanningDesiredInputs(t, fixture, inputs, nil)
			},
			decision: DecisionUpdate,
			reasons:  []string{},
		},
		{
			name: "ordinary drift update",
			change: func(t *testing.T, fixture *resourcePlanningFixture) {
				outputs := *fixture.request.RecordedObservation.Outputs
				outputs.Status = &ruleStatus{Code: "degraded"}
				setPlanningObservation(t, fixture, fixture.request.RecordedObservation, &outputs)
			},
			decision: DecisionUpdate,
			reasons:  []string{},
		},
		{
			name:     "no-op",
			change:   func(*testing.T, *resourcePlanningFixture) {},
			decision: DecisionNoOp,
			reasons:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newResourcePlanningFixture(t, validRuleDefinition())
			tt.change(t, &fixture)
			operation := planFixtureOperation(t, fixture)
			require.Equal(t, tt.decision, operation.Decision)
			require.Equal(t, tt.reasons, operation.Reasons)
		})
	}
}

func TestResourcePlanningUsesSemanticInputEquality(t *testing.T) {
	definition := validRuleDefinition()
	_, _, labels, _, _, _ := ruleInputFields()
	definition.InputSemantics.Rules = []InputRule[ruleInput]{
		EqualBy(labels, func(_, _ map[string]string) bool { return true }),
	}
	fixture := newResourcePlanningFixture(t, definition)
	inputs := fixture.request.Desired.Inputs
	inputs.Labels = map[string]string{"env": "production"}
	setPlanningDesiredInputs(t, &fixture, inputs, nil)

	operation := planFixtureOperation(t, fixture)
	require.Equal(t, DecisionNoOp, operation.Decision)
	require.Empty(t, operation.Reasons)
}

func TestResourcePlanningAggregatesReplacementReasons(t *testing.T) {
	definition := validRuleDefinition()
	_, _, _, settings, _, replicas := ruleInputFields()
	status := OutputField(func(value *ruleOutput) **ruleStatus { return &value.Status })
	statusCode := OutputField(func(value *ruleOutput) *string { return &value.Status.Code })
	definition.Replacement.Inputs = []ReplacementRule[ruleInput]{
		ReplaceWhenChanged(settings),
		ReplaceWhenChanged(replicas),
	}
	definition.Replacement.Drift = []DriftRule[*ruleOutput]{
		ReplaceOnDrift(status, func(a, b *ruleStatus) bool { return a.Code == b.Code }),
		ReplaceOnDrift(statusCode, func(a, b string) bool { return a == b }),
	}
	fixture := newResourcePlanningFixture(t, definition)
	inputs := fixture.request.Desired.Inputs
	inputs.Settings = &ruleSettings{Tier: "premium", Replicas: 4}
	setPlanningDesiredInputs(t, &fixture, inputs, nil)
	outputs := *fixture.request.RecordedObservation.Outputs
	outputs.Status = &ruleStatus{Code: "degraded"}
	setPlanningObservation(t, &fixture, fixture.request.RecordedObservation, &outputs)

	operation := planFixtureOperation(t, fixture)
	require.Equal(t, DecisionReplace, operation.Decision)
	require.Equal(t, []string{
		"drift:status",
		"drift:status.code",
		"input:settings",
		"input:settings.replicas",
	}, operation.Reasons)
}

func TestResourcePlanningClassifiesPendingInputsConservatively(t *testing.T) {
	pending, err := PendingEncodedValue([]string{"resource.source.value"})
	require.NoError(t, err)
	tests := []struct {
		name     string
		field    string
		decision Decision
		reasons  []string
	}{
		{name: "address", field: "name", decision: DecisionReplace, reasons: []string{"address:name"}},
		{
			name:     "replacement",
			field:    "region",
			decision: DecisionReplace,
			reasons:  []string{"input:region"},
		},
		{name: "ordinary", field: "zones", decision: DecisionUpdate, reasons: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newResourcePlanningFixture(t, validRuleDefinition())
			setPlanningDesiredInputs(
				t,
				&fixture,
				fixture.request.Desired.Inputs,
				map[string]EncodedValue{tt.field: pending},
			)
			operation := planFixtureOperation(t, fixture)
			require.Equal(t, tt.decision, operation.Decision)
			require.Equal(t, tt.reasons, operation.Reasons)
		})
	}
}

func TestResourcePlanningValidatesGlobalConfigurationObservation(t *testing.T) {
	definition := validRuleDefinition()
	definition.Identity.Scope = IdentityGlobal
	fixture := newResourcePlanningFixture(t, definition)
	record := planningConfigurationRecord(
		t,
		configurationLibraryPath,
		"https://other.example",
	)
	fixture.request.Desired.Target.Configuration.Record = new(record)

	_, err := fixture.definition.planResourceOperation(fixture.request)
	require.ErrorContains(t, err, "global configuration change requires desired observation")

	fixture.request.DesiredConfigurationObservation =
		&preparedResourceObservation[*ruleOutput]{
			Observation: ResourceObservation{Status: ObservationAbsent},
		}
	_, err = fixture.definition.planResourceOperation(fixture.request)
	require.ErrorContains(t, err, "desired configuration read returned not found")

	fixture.request.DesiredConfigurationObservation =
		&preparedResourceObservation[*ruleOutput]{}
	setPlanningObservation(
		t,
		&fixture,
		fixture.request.DesiredConfigurationObservation,
		fixture.request.Prior.Outputs,
	)
	operation := planFixtureOperation(t, fixture)
	require.Equal(t, DecisionNoOp, operation.Decision)
	require.Equal(
		t,
		fixture.request.DesiredConfigurationObservation.Observation,
		*operation.Observation,
	)
}

func TestResourcePlanningRejectsUnpreparedStateAndChangedIdentity(t *testing.T) {
	fixture := newResourcePlanningFixture(t, validRuleDefinition())
	fixture.request.Prior.Target.SchemaVersion--
	_, err := fixture.definition.planResourceOperation(fixture.request)
	require.ErrorContains(t, err, "requires migration")

	fixture = newResourcePlanningFixture(t, validRuleDefinition())
	outputs := *fixture.request.RecordedObservation.Outputs
	outputs.ID = "bucket-2"
	setPlanningObservation(t, &fixture, fixture.request.RecordedObservation, &outputs)
	_, err = fixture.definition.planResourceOperation(fixture.request)
	require.ErrorContains(t, err, `recorded stable ID "bucket-1"`)
	require.ErrorContains(t, err, `observed stable ID "bucket-2"`)
}

func TestResourcePlanningUsesCanonicalNumberEquality(t *testing.T) {
	positiveZero, err := NumberValue(0)
	require.NoError(t, err)
	negativeZero, err := NumberValue(math.Copysign(0, -1))
	require.NoError(t, err)
	require.False(t, encodedValuesEqual(positiveZero, negativeZero))
}
