package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func planningRequestFromFixture(fixture resourcePlanningFixture) resourcePlanningRequest {
	request := resourcePlanningRequest{}
	if fixture.request.Desired != nil {
		target := fixture.request.Desired.Target
		request.Desired = &target
	}
	if fixture.request.Prior != nil {
		target := fixture.request.Prior.Target
		request.Prior = &target
	}
	if fixture.request.RecordedObservation != nil {
		observation := cloneResourceObservation(
			fixture.request.RecordedObservation.Observation,
		)
		request.RecordedObservation = &observation
	}
	if fixture.request.DesiredConfigurationObservation != nil {
		observation := cloneResourceObservation(
			fixture.request.DesiredConfigurationObservation.Observation,
		)
		request.DesiredConfigurationObservation = &observation
	}
	return request
}

func TestResourcePlanningPreparesMigratedTarget(t *testing.T) {
	calls := []string{}
	definition := validRuleDefinition()
	definition.SchemaVersion = 3
	definition.Migrate = func(
		oldVersion int,
		prior ResourceMigrationState,
	) (ResourceMigrationState, error) {
		calls = append(calls, "resource")
		require.Equal(t, 1, oldVersion)
		return prior, nil
	}
	definition.Identity.Version = 2
	definition.Identity.Migrate = func(
		oldVersion int,
		resource ResourceMigrationState,
		priorStableID *string,
	) (*string, error) {
		calls = append(calls, "identity")
		require.Equal(t, 1, oldVersion)
		require.Equal(t, "bucket-1", stringValue(priorStableID))
		require.Equal(t, EncodedValueObject, resource.Inputs.Kind())
		require.Equal(t, EncodedValueObject, resource.Outputs.Kind())
		return priorStableID, nil
	}

	fixture := newResourcePlanningFixture(t, definition)
	fixture.request.Prior.Target.SchemaVersion = 1
	fixture.request.Prior.Target.Identity.Version = 1
	request := planningRequestFromFixture(fixture)

	operation, err := fixture.definition.planResourceOperation(request)
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, operation.Decision)
	require.Equal(t, []string{"resource", "identity"}, calls)
	require.Equal(t, 3, operation.Prior.SchemaVersion)
	require.Equal(t, 2, operation.Prior.Identity.Version)
	require.Equal(t, 1, request.Prior.SchemaVersion)
	require.Equal(t, 1, request.Prior.Identity.Version)
}

func TestResourcePlanningUsesPendingEncodedInputs(t *testing.T) {
	fixture := newResourcePlanningFixture(t, validRuleDefinition())
	fields, ok := fixture.request.Desired.Target.Inputs.ObjectFields()
	require.True(t, ok)
	pending, err := PendingEncodedValue([]string{"resource.region.id"})
	require.NoError(t, err)
	fields["region"] = pending
	fixture.request.Desired.Target.Inputs, err = ObjectValue(fields)
	require.NoError(t, err)

	operation, err := fixture.definition.planResourceOperation(
		planningRequestFromFixture(fixture),
	)
	require.NoError(t, err)
	require.Equal(t, DecisionReplace, operation.Decision)
	require.Equal(t, []string{"input:region"}, operation.Reasons)
}

func TestResourcePlanningRejectsInvalidMigratedValues(t *testing.T) {
	identityCalls := 0
	definition := validRuleDefinition()
	definition.SchemaVersion = 3
	definition.Migrate = func(
		_ int,
		prior ResourceMigrationState,
	) (ResourceMigrationState, error) {
		prior.Outputs = StringValue("invalid")
		return prior, nil
	}
	definition.Identity.Version = 2
	definition.Identity.Migrate = func(
		int,
		ResourceMigrationState,
		*string,
	) (*string, error) {
		identityCalls++
		return nil, nil
	}
	fixture := newResourcePlanningFixture(t, definition)
	fixture.request.Prior.Target.SchemaVersion = 1
	fixture.request.Prior.Target.Identity.Version = 1

	_, err := fixture.definition.planResourceOperation(
		planningRequestFromFixture(fixture),
	)
	require.ErrorContains(t, err, "recorded resource outputs: expected object")
	require.Zero(t, identityCalls)
}

func TestResourcePlanningRejectsUnusablePriorVersions(t *testing.T) {
	t.Run("resource migration missing", func(t *testing.T) {
		fixture := newResourcePlanningFixture(t, validRuleDefinition())
		fixture.request.Prior.Target.SchemaVersion = 1

		_, err := fixture.definition.planResourceOperation(
			planningRequestFromFixture(fixture),
		)
		require.ErrorContains(t, err, "no resource migration registered for version 1")
	})

	t.Run("resource version newer", func(t *testing.T) {
		fixture := newResourcePlanningFixture(t, validRuleDefinition())
		fixture.request.Prior.Target.SchemaVersion = 3

		_, err := fixture.definition.planResourceOperation(
			planningRequestFromFixture(fixture),
		)
		require.ErrorContains(
			t,
			err,
			"recorded resource schema version 3 is newer than registered version 2",
		)
	})

	t.Run("identity migration missing", func(t *testing.T) {
		definition := validRuleDefinition()
		definition.Identity.Version = 2
		fixture := newResourcePlanningFixture(t, definition)
		fixture.request.Prior.Target.Identity.Version = 1

		_, err := fixture.definition.planResourceOperation(
			planningRequestFromFixture(fixture),
		)
		require.ErrorContains(t, err, "no identity migration registered for version 1")
	})
}

func TestResourcePlanningRejectsInvalidObservationOutputs(t *testing.T) {
	fixture := newResourcePlanningFixture(t, validRuleDefinition())
	outputs, err := ObjectValue(map[string]EncodedValue{
		"id": StringValue("bucket-1"),
	})
	require.NoError(t, err)
	fixture.request.RecordedObservation.Observation.Outputs = &outputs

	_, err = fixture.definition.planResourceOperation(
		planningRequestFromFixture(fixture),
	)
	require.ErrorContains(t, err, `field "etag" is missing`)
}

func TestResourcePlanningReportsMigrationPanic(t *testing.T) {
	definition := validRuleDefinition()
	definition.Migrate = func(
		int,
		ResourceMigrationState,
	) (ResourceMigrationState, error) {
		panic("migration failed")
	}
	fixture := newResourcePlanningFixture(t, definition)
	fixture.request.Prior.Target.SchemaVersion = 1

	_, err := fixture.definition.planResourceOperation(
		planningRequestFromFixture(fixture),
	)
	var panicErr *PanicError
	require.ErrorAs(t, err, &panicErr)
	require.Equal(t, "migration failed", panicErr.Value)
}
