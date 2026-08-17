package runtime

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

var errResourceApplyTest = errors.New("provider failed")

type resourceApplyHarness struct {
	calls             []string
	validateErr       error
	readDesiredErr    error
	readPriorErr      error
	createErr         error
	updateErr         error
	deleteErr         error
	persistErr        error
	readDesiredResult preparedResourceApplyResult[*ruleOutput]
	readDesiredInputs ruleInput
	readDesiredPrior  *ruleOutput
	readPriorResult   ResourceObservation
	createResult      preparedResourceApplyResult[*ruleOutput]
	updateResult      preparedResourceApplyResult[*ruleOutput]
	updatePrior       Prior[ruleInput, *ruleOutput]
	deletedOutputs    EncodedValue
	persisted         *ResourceTarget
	removed           bool
}

func (h *resourceApplyHarness) callbacks() resourceApplyCallbacks[
	ruleInput,
	*ruleOutput,
	NoConfig,
] {
	return resourceApplyCallbacks[ruleInput, *ruleOutput, NoConfig]{
		ReadDesired: func(
			_ context.Context,
			inputs ruleInput,
			_ NoConfig,
			prior *ruleOutput,
		) (preparedResourceApplyResult[*ruleOutput], error) {
			h.calls = append(h.calls, "read-desired")
			h.readDesiredInputs = inputs
			h.readDesiredPrior = prior
			return h.readDesiredResult, h.readDesiredErr
		},
		ReadPrior: func(context.Context) (ResourceObservation, error) {
			h.calls = append(h.calls, "read-prior")
			return h.readPriorResult, h.readPriorErr
		},
		Create: func(
			context.Context,
			ruleInput,
			NoConfig,
		) (preparedResourceApplyResult[*ruleOutput], error) {
			h.calls = append(h.calls, "create")
			return h.createResult, h.createErr
		},
		Update: func(
			_ context.Context,
			_ ruleInput,
			_ NoConfig,
			prior Prior[ruleInput, *ruleOutput],
		) (preparedResourceApplyResult[*ruleOutput], error) {
			h.calls = append(h.calls, "update")
			h.updatePrior = prior
			return h.updateResult, h.updateErr
		},
		DeletePrior: func(_ context.Context, outputs EncodedValue) error {
			h.calls = append(h.calls, "delete")
			h.deletedOutputs = outputs
			return h.deleteErr
		},
		Persist: func(_ context.Context, target *ResourceTarget) error {
			h.calls = append(h.calls, "persist")
			if h.persistErr != nil {
				return h.persistErr
			}
			if target == nil {
				h.removed = true
			} else {
				copy := *target
				h.persisted = &copy
			}
			return nil
		},
	}
}

type resourceApplyCase struct {
	definition resolvedResourceDefinition[ruleInput, *ruleOutput, NoConfig]
	request    resourceApplyRequest[ruleInput, *ruleOutput, NoConfig]
	harness    *resourceApplyHarness
}

func newResourceApplyCase(
	t *testing.T,
	definition ruleDefinition,
	change func(*testing.T, *resourcePlanningFixture),
) resourceApplyCase {
	t.Helper()
	harness := &resourceApplyHarness{}
	definition.Validate = func(context.Context, ruleInput, NoConfig) error {
		harness.calls = append(harness.calls, "validate")
		return harness.validateErr
	}
	fixture := newResourcePlanningFixture(t, definition)
	if change != nil {
		change(t, &fixture)
	}
	operation := planFixtureOperation(t, fixture)
	require.NotNil(t, operation)

	createOutputs := &ruleOutput{
		ID:     "bucket-2",
		ETag:   "etag-created",
		Status: &ruleStatus{Code: "ready"},
	}
	priorOutputs := &ruleOutput{
		ID:     "bucket-1",
		ETag:   "etag-1",
		Status: &ruleStatus{Code: "ready"},
	}
	if fixture.request.Prior != nil {
		priorOutputs = fixture.request.Prior.Outputs
	}
	updateOutputs := &ruleOutput{
		ID:     priorOutputs.ID,
		ETag:   "etag-updated",
		Status: &ruleStatus{Code: "ready"},
	}
	harness.createResult = preparedResourceResult(t, createOutputs)
	harness.updateResult = preparedResourceResult(t, updateOutputs)
	harness.readDesiredResult = preparedResourceResult(
		t,
		priorOutputs,
	)
	if operation.Observation != nil {
		harness.readPriorResult = *operation.Observation
	}

	return resourceApplyCase{
		definition: fixture.definition,
		request: resourceApplyRequest[ruleInput, *ruleOutput, NoConfig]{
			Address:              "resource.logs",
			Operation:            *operation,
			Desired:              fixture.request.Desired,
			DesiredConfiguration: NoConfig{},
			Prior:                fixture.request.Prior,
			Observation:          fixture.request.RecordedObservation,
			DependsOn:            []string{"resource.network"},
		},
		harness: harness,
	}
}

func preparedResourceResult(
	t *testing.T,
	outputs *ruleOutput,
) preparedResourceApplyResult[*ruleOutput] {
	t.Helper()
	return preparedResourceApplyResult[*ruleOutput]{
		Outputs: outputs,
		Encoded: encodeRuleOutput(t, outputs),
	}
}

func (c resourceApplyCase) apply(t *testing.T) (*ResourceTarget, error) {
	t.Helper()
	return c.definition.applyResourceOperation(
		context.Background(),
		c.request,
		c.harness.callbacks(),
	)
}

func TestResourceApplyCreatesAndPersistsTarget(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.Prior = nil
			fixture.request.RecordedObservation = nil
		},
	)

	target, err := testCase.apply(t)
	require.NoError(t, err)
	require.Equal(t, []string{"validate", "create", "persist"}, testCase.harness.calls)
	require.Equal(t, target, testCase.harness.persisted)
	require.Equal(t, testCase.request.Desired.Target.Binding, target.Binding)
	require.Equal(t, testCase.definition.schemaVersion, target.SchemaVersion)
	require.True(t, encodedValuesEqual(testCase.request.Desired.Target.Inputs, target.Inputs))
	require.True(t, encodedValuesEqual(testCase.harness.createResult.Encoded, target.Outputs))
	require.Equal(t, []string{"resource.network"}, target.DependsOn)
	require.Equal(t, "bucket-2", stringValue(target.Identity.StableID))
}

func TestResourceApplyUpdatesFromSavedObservationWithoutRead(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(t *testing.T, fixture *resourcePlanningFixture) {
			inputs := fixture.request.Desired.Inputs
			inputs.Zones = []string{"a", "c"}
			setPlanningDesiredInputs(t, fixture, inputs, nil)
		},
	)

	target, err := testCase.apply(t)
	require.NoError(t, err)
	require.Equal(t, []string{"validate", "update", "persist"}, testCase.harness.calls)
	require.Equal(t, testCase.request.Prior.Inputs, testCase.harness.updatePrior.Inputs)
	require.Equal(t, testCase.request.Prior.Outputs, testCase.harness.updatePrior.Outputs)
	require.Equal(t, testCase.request.Observation.Outputs, testCase.harness.updatePrior.Observed)
	require.Equal(t, "bucket-1", stringValue(target.Identity.StableID))
	require.True(t, encodedValuesEqual(testCase.harness.updateResult.Encoded, target.Outputs))
}

func TestResourceApplyRejectsChangedUpdateStableID(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(t *testing.T, fixture *resourcePlanningFixture) {
			inputs := fixture.request.Desired.Inputs
			inputs.Zones = []string{"a", "c"}
			setPlanningDesiredInputs(t, fixture, inputs, nil)
		},
	)
	testCase.harness.updateResult = preparedResourceResult(t, &ruleOutput{ID: "bucket-other"})

	_, err := testCase.apply(t)
	require.ErrorContains(t, err, `prior stable ID "bucket-1"`)
	require.ErrorContains(t, err, `updated stable ID "bucket-other"`)
	require.Equal(t, []string{"validate", "update"}, testCase.harness.calls)
	require.Nil(t, testCase.harness.persisted)
}

func TestResourceApplyUpdateRecordsLogicalAddressIdentity(t *testing.T) {
	definition := validRuleDefinition()
	definition.Identity.StableID = nil
	definition.Replacement.Drift = nil
	testCase := newResourceApplyCase(
		t,
		definition,
		func(t *testing.T, fixture *resourcePlanningFixture) {
			inputs := fixture.request.Desired.Inputs
			inputs.Zones = []string{"a", "c"}
			setPlanningDesiredInputs(t, fixture, inputs, nil)
		},
	)

	target, err := testCase.apply(t)
	require.NoError(t, err)
	require.Equal(t, []string{"validate", "update", "persist"}, testCase.harness.calls)
	require.Nil(t, target.Identity.StableID)
}

func TestResourceApplyNoOpPersistsExactDesiredInputs(t *testing.T) {
	definition := validRuleDefinition()
	_, _, labels, _, _, _ := ruleInputFields()
	definition.InputSemantics.Rules = []InputRule[ruleInput]{
		EqualBy(labels, func(_, _ map[string]string) bool { return true }),
	}
	testCase := newResourceApplyCase(
		t,
		definition,
		func(t *testing.T, fixture *resourcePlanningFixture) {
			inputs := fixture.request.Desired.Inputs
			inputs.Labels = map[string]string{"env": "production"}
			setPlanningDesiredInputs(t, fixture, inputs, nil)
		},
	)

	target, err := testCase.apply(t)
	require.NoError(t, err)
	require.Equal(t, []string{"persist"}, testCase.harness.calls)
	require.True(t, encodedValuesEqual(testCase.request.Desired.Target.Inputs, target.Inputs))
	require.True(t, encodedValuesEqual(
		*testCase.request.Observation.Observation.Outputs,
		target.Outputs,
	))
	require.Equal(t, *testCase.request.Observation.Observation.Identity, target.Identity)
}

func TestResourceApplyRejectsChangedKnownPremise(t *testing.T) {
	testCase := newResourceApplyCase(t, validRuleDefinition(), nil)
	inputs := testCase.request.Desired.Inputs
	inputs.Region = "us-west-2"
	testCase.request.Desired = &preparedResourceDesired[ruleInput]{
		Inputs: inputs,
		Target: testCase.request.Desired.Target,
	}
	testCase.request.Desired.Target.Inputs = encodeRuleInput(t, inputs, nil)

	_, err := testCase.apply(t)
	require.ErrorContains(t, err, "desired inputs do not match the saved plan")
	require.Empty(t, testCase.harness.calls)
}

func TestResourceApplyResolvesPendingInputAndRechecksDecision(t *testing.T) {
	pending, err := PendingEncodedValue([]string{"resource.source.value"})
	require.NoError(t, err)
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(t *testing.T, fixture *resourcePlanningFixture) {
			setPlanningDesiredInputs(
				t,
				fixture,
				fixture.request.Desired.Inputs,
				map[string]EncodedValue{"zones": pending},
			)
		},
	)
	resolved := testCase.request.Prior.Inputs
	testCase.request.Desired = &preparedResourceDesired[ruleInput]{
		Inputs: resolved,
		Target: testCase.request.Desired.Target,
	}
	testCase.request.Desired.Target.Inputs = encodeRuleInput(t, resolved, nil)

	_, err = testCase.apply(t)
	require.ErrorContains(t, err, "resource decision changed from update to no-op")
	require.Empty(t, testCase.harness.calls)

	testCase = newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(t *testing.T, fixture *resourcePlanningFixture) {
			setPlanningDesiredInputs(
				t,
				fixture,
				fixture.request.Desired.Inputs,
				map[string]EncodedValue{"zones": pending},
			)
		},
	)
	resolved = testCase.request.Desired.Inputs
	resolved.Zones = []string{"a", "c"}
	testCase.request.Desired = &preparedResourceDesired[ruleInput]{
		Inputs: resolved,
		Target: testCase.request.Desired.Target,
	}
	testCase.request.Desired.Target.Inputs = encodeRuleInput(t, resolved, nil)

	_, err = testCase.apply(t)
	require.NoError(t, err)
	require.Equal(t, []string{"validate", "update", "persist"}, testCase.harness.calls)
}

func TestResourceApplyRejectsReintroducedDestroyTarget(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.Desired = nil
		},
	)
	desired := PlannedResourceTarget{
		Binding: testCase.request.Prior.Target.Binding,
		Inputs:  testCase.request.Prior.Target.Inputs,
		Configuration: PlannedConfiguration{
			Kind:   PlannedConfigurationConcrete,
			Record: new(testCase.request.Prior.Target.Configuration),
		},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	testCase.request.Desired = &preparedResourceDesired[ruleInput]{
		Target: desired,
		Inputs: testCase.request.Prior.Inputs,
	}

	_, err := testCase.apply(t)
	require.ErrorContains(t, err, "saved destroy requires the resource to remain absent")
	require.Empty(t, testCase.harness.calls)
}

func TestResourceApplyValidatesPendingGlobalConfiguration(t *testing.T) {
	definition := validRuleDefinition()
	definition.Identity.Scope = IdentityGlobal
	testCase := newResourceApplyCase(
		t,
		definition,
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.Desired.Target.Configuration = pendingOperationConfiguration()
		},
	)
	record := planningConfigurationRecord(
		t,
		configurationLibraryPath,
		"https://other.example",
	)
	testCase.request.Desired = &preparedResourceDesired[ruleInput]{
		Inputs: testCase.request.Desired.Inputs,
		Target: testCase.request.Desired.Target,
	}
	testCase.request.Desired.Target.Configuration = PlannedConfiguration{
		Kind:   PlannedConfigurationConcrete,
		Record: new(record),
	}

	_, err := testCase.apply(t)
	require.ErrorContains(t, err, "resource decision changed from replace to no-op")
	require.Equal(t, []string{"read-desired"}, testCase.harness.calls)
	require.Equal(t, testCase.request.Prior.Inputs, testCase.harness.readDesiredInputs)
	require.Equal(t, testCase.request.Prior.Outputs, testCase.harness.readDesiredPrior)

	testCase = newResourceApplyCase(
		t,
		definition,
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.Desired.Target.Configuration = pendingOperationConfiguration()
		},
	)
	testCase.request.Desired = &preparedResourceDesired[ruleInput]{
		Inputs: testCase.request.Desired.Inputs,
		Target: testCase.request.Desired.Target,
	}
	testCase.request.Desired.Target.Configuration = PlannedConfiguration{
		Kind:   PlannedConfigurationConcrete,
		Record: new(record),
	}
	testCase.harness.readDesiredResult = preparedResourceResult(
		t,
		&ruleOutput{ID: "bucket-other"},
	)

	_, err = testCase.apply(t)
	require.ErrorContains(t, err, `recorded stable ID "bucket-1"`)
	require.Equal(t, []string{"read-desired"}, testCase.harness.calls)

	testCase = newResourceApplyCase(
		t,
		definition,
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.Desired.Target.Configuration = pendingOperationConfiguration()
		},
	)
	testCase.request.Desired = &preparedResourceDesired[ruleInput]{
		Inputs: testCase.request.Desired.Inputs,
		Target: testCase.request.Desired.Target,
	}
	testCase.request.Desired.Target.Configuration = PlannedConfiguration{
		Kind:   PlannedConfigurationConcrete,
		Record: new(record),
	}
	testCase.harness.readDesiredErr = ErrNotFound

	_, err = testCase.apply(t)
	require.ErrorContains(t, err, "desired configuration read returned not found")
	require.Equal(t, []string{"read-desired"}, testCase.harness.calls)

	logicalDefinition := validRuleDefinition()
	logicalDefinition.Identity.Scope = IdentityGlobal
	logicalDefinition.Identity.StableID = nil
	logicalDefinition.Replacement.Drift = nil
	testCase = newResourceApplyCase(
		t,
		logicalDefinition,
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.Desired.Target.Configuration = pendingOperationConfiguration()
		},
	)
	testCase.request.Desired = &preparedResourceDesired[ruleInput]{
		Inputs: testCase.request.Desired.Inputs,
		Target: testCase.request.Desired.Target,
	}
	testCase.request.Desired.Target.Configuration = PlannedConfiguration{
		Kind:   PlannedConfigurationConcrete,
		Record: new(record),
	}

	_, err = testCase.apply(t)
	require.ErrorContains(t, err, "resource decision changed from replace to no-op")
	require.Equal(t, []string{"read-desired"}, testCase.harness.calls)
}

func TestResourceApplyUsesResolvedGlobalConfigurationForReplacement(t *testing.T) {
	definition := validRuleDefinition()
	definition.Identity.Scope = IdentityGlobal
	testCase := newResourceApplyCase(
		t,
		definition,
		func(t *testing.T, fixture *resourcePlanningFixture) {
			inputs := fixture.request.Desired.Inputs
			inputs.Region = "us-west-2"
			setPlanningDesiredInputs(t, fixture, inputs, nil)
			fixture.request.Desired.Target.Configuration = pendingOperationConfiguration()
		},
	)
	record := planningConfigurationRecord(
		t,
		configurationLibraryPath,
		"https://other.example",
	)
	testCase.request.Desired = &preparedResourceDesired[ruleInput]{
		Inputs: testCase.request.Desired.Inputs,
		Target: testCase.request.Desired.Target,
	}
	testCase.request.Desired.Target.Configuration = PlannedConfiguration{
		Kind:   PlannedConfigurationConcrete,
		Record: new(record),
	}

	target, err := testCase.apply(t)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{
			"read-desired",
			"validate",
			"read-prior",
			"delete",
			"create",
			"persist",
		},
		testCase.harness.calls,
	)
	require.Equal(t, "bucket-2", stringValue(target.Identity.StableID))
}

func TestResourceApplyReplacesChangedBinding(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.Desired.Target.Binding.Export = "archive"
		},
	)

	target, err := testCase.apply(t)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"validate", "read-prior", "delete", "create", "persist"},
		testCase.harness.calls,
	)
	require.Equal(t, "archive", target.Binding.Export)
	require.NotEqual(
		t,
		testCase.request.Prior.Target.Identity.DefinitionDigest,
		target.Identity.DefinitionDigest,
	)
}

func TestResourceApplyReplacesAfterFreshIdentityCheck(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(t *testing.T, fixture *resourcePlanningFixture) {
			inputs := fixture.request.Desired.Inputs
			inputs.Region = "us-west-2"
			setPlanningDesiredInputs(t, fixture, inputs, nil)
		},
	)
	freshOutputs := &ruleOutput{
		ID:     "bucket-1",
		ETag:   "etag-fresh",
		Status: &ruleStatus{Code: "degraded"},
	}
	freshEncoded := encodeRuleOutput(t, freshOutputs)
	freshIdentity, err := testCase.definition.newIdentityRecord(
		testCase.request.Prior.Target.Binding.LibraryPath,
		testCase.request.Prior.Target.Binding.Export,
		testCase.request.Prior.Inputs,
		freshOutputs,
	)
	require.NoError(t, err)
	testCase.harness.readPriorResult = ResourceObservation{
		Status:   ObservationPresent,
		Outputs:  new(freshEncoded),
		Identity: new(freshIdentity),
	}

	target, err := testCase.apply(t)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"validate", "read-prior", "delete", "create", "persist"},
		testCase.harness.calls,
	)
	require.True(t, encodedValuesEqual(freshEncoded, testCase.harness.deletedOutputs))
	require.Equal(t, "bucket-2", stringValue(target.Identity.StableID))
}

func TestResourceApplySkipsReplacementDeleteWhenPriorIsAbsent(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(t *testing.T, fixture *resourcePlanningFixture) {
			inputs := fixture.request.Desired.Inputs
			inputs.Region = "us-west-2"
			setPlanningDesiredInputs(t, fixture, inputs, nil)
		},
	)
	testCase.harness.readPriorErr = ErrNotFound

	_, err := testCase.apply(t)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"validate", "read-prior", "create", "persist"},
		testCase.harness.calls,
	)
}

func TestResourceApplyRejectsFreshStableIDMismatch(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(t *testing.T, fixture *resourcePlanningFixture) {
			inputs := fixture.request.Desired.Inputs
			inputs.Region = "us-west-2"
			setPlanningDesiredInputs(t, fixture, inputs, nil)
		},
	)
	freshOutputs := &ruleOutput{ID: "bucket-other"}
	freshEncoded := encodeRuleOutput(t, freshOutputs)
	freshIdentity, err := testCase.definition.newIdentityRecord(
		testCase.request.Prior.Target.Binding.LibraryPath,
		testCase.request.Prior.Target.Binding.Export,
		testCase.request.Prior.Inputs,
		freshOutputs,
	)
	require.NoError(t, err)
	testCase.harness.readPriorResult = ResourceObservation{
		Status:   ObservationPresent,
		Outputs:  new(freshEncoded),
		Identity: new(freshIdentity),
	}

	_, err = testCase.apply(t)
	require.ErrorContains(t, err, `recorded stable ID "bucket-1"`)
	require.Equal(t, []string{"validate", "read-prior"}, testCase.harness.calls)
}

func TestResourceApplyRejectsInvalidCreatedIdentity(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(t *testing.T, fixture *resourcePlanningFixture) {
			inputs := fixture.request.Desired.Inputs
			inputs.Region = "us-west-2"
			setPlanningDesiredInputs(t, fixture, inputs, nil)
		},
	)
	testCase.harness.createResult = preparedResourceResult(t, &ruleOutput{})

	_, err := testCase.apply(t)
	require.ErrorContains(t, err, "stable ID must not be empty")
	require.Equal(
		t,
		[]string{"validate", "read-prior", "delete", "create"},
		testCase.harness.calls,
	)
	require.Nil(t, testCase.harness.persisted)
}

func TestResourceApplyDeletesLogicalAddressResource(t *testing.T) {
	definition := validRuleDefinition()
	definition.Identity.StableID = nil
	definition.Replacement.Drift = nil
	testCase := newResourceApplyCase(
		t,
		definition,
		func(t *testing.T, fixture *resourcePlanningFixture) {
			inputs := fixture.request.Desired.Inputs
			inputs.Region = "us-west-2"
			setPlanningDesiredInputs(t, fixture, inputs, nil)
		},
	)

	_, err := testCase.apply(t)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"validate", "read-prior", "delete", "create", "persist"},
		testCase.harness.calls,
	)
	require.Nil(t, testCase.harness.persisted.Identity.StableID)
}

func TestResourceApplyRereadsRemoteMissingCreate(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.RecordedObservation = &preparedResourceObservation[*ruleOutput]{
				Observation: ResourceObservation{Status: ObservationAbsent},
			}
		},
	)
	testCase.harness.readPriorErr = ErrNotFound

	_, err := testCase.apply(t)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"validate", "read-prior", "create", "persist"},
		testCase.harness.calls,
	)

	testCase = newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.RecordedObservation = &preparedResourceObservation[*ruleOutput]{
				Observation: ResourceObservation{Status: ObservationAbsent},
			}
		},
	)
	present := newResourcePlanningFixture(t, validRuleDefinition())
	testCase.harness.readPriorResult = present.request.RecordedObservation.Observation

	_, err = testCase.apply(t)
	require.ErrorContains(t, err, "remote object reappeared after planning")
	require.Equal(t, []string{"validate", "read-prior"}, testCase.harness.calls)
}

func TestResourceApplyDestroyReadsBeforeDelete(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.Desired = nil
		},
	)

	target, err := testCase.apply(t)
	require.NoError(t, err)
	require.Nil(t, target)
	require.True(t, testCase.harness.removed)
	require.Equal(t, []string{"read-prior", "delete", "persist"}, testCase.harness.calls)

	testCase = newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.Desired = nil
		},
	)
	testCase.harness.readPriorErr = ErrNotFound

	_, err = testCase.apply(t)
	require.NoError(t, err)
	require.True(t, testCase.harness.removed)
	require.Equal(t, []string{"read-prior", "persist"}, testCase.harness.calls)
}

func TestResourceApplyDestroyDeletesObjectThatReappeared(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.Desired = nil
			fixture.request.RecordedObservation = &preparedResourceObservation[*ruleOutput]{
				Observation: ResourceObservation{Status: ObservationAbsent},
			}
		},
	)
	present := newResourcePlanningFixture(t, validRuleDefinition())
	testCase.harness.readPriorResult = present.request.RecordedObservation.Observation

	_, err := testCase.apply(t)
	require.NoError(t, err)
	require.True(t, testCase.harness.removed)
	require.Equal(t, []string{"read-prior", "delete", "persist"}, testCase.harness.calls)
}

func TestResourceApplyRejectsInvalidDependenciesBeforeProviderCalls(t *testing.T) {
	testCase := newResourceApplyCase(
		t,
		validRuleDefinition(),
		func(_ *testing.T, fixture *resourcePlanningFixture) {
			fixture.request.Prior = nil
			fixture.request.RecordedObservation = nil
		},
	)
	testCase.request.DependsOn = []string{"resource.z", "resource.a"}

	_, err := testCase.apply(t)
	require.ErrorContains(t, err, "dependencies must be unique and sorted")
	require.Empty(t, testCase.harness.calls)
}

func TestResourceApplyFailureStopsRemainingWork(t *testing.T) {
	newReplacement := func(t *testing.T) resourceApplyCase {
		return newResourceApplyCase(
			t,
			validRuleDefinition(),
			func(t *testing.T, fixture *resourcePlanningFixture) {
				inputs := fixture.request.Desired.Inputs
				inputs.Region = "us-west-2"
				setPlanningDesiredInputs(t, fixture, inputs, nil)
			},
		)
	}
	tests := []struct {
		name      string
		fail      func(*resourceApplyHarness)
		wantCalls []string
	}{
		{
			name: "validation",
			fail: func(h *resourceApplyHarness) {
				h.validateErr = errResourceApplyTest
			},
			wantCalls: []string{"validate"},
		},
		{
			name: "read",
			fail: func(h *resourceApplyHarness) {
				h.readPriorErr = errResourceApplyTest
			},
			wantCalls: []string{"validate", "read-prior"},
		},
		{
			name: "delete",
			fail: func(h *resourceApplyHarness) {
				h.deleteErr = errResourceApplyTest
			},
			wantCalls: []string{"validate", "read-prior", "delete"},
		},
		{
			name: "create",
			fail: func(h *resourceApplyHarness) {
				h.createErr = errResourceApplyTest
			},
			wantCalls: []string{"validate", "read-prior", "delete", "create"},
		},
		{
			name: "persist",
			fail: func(h *resourceApplyHarness) {
				h.persistErr = errResourceApplyTest
			},
			wantCalls: []string{
				"validate",
				"read-prior",
				"delete",
				"create",
				"persist",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCase := newReplacement(t)
			tt.fail(testCase.harness)

			_, err := testCase.apply(t)
			require.ErrorIs(t, err, errResourceApplyTest)
			require.True(t, slices.Equal(tt.wantCalls, testCase.harness.calls))
			require.Nil(t, testCase.harness.persisted)
		})
	}
}
