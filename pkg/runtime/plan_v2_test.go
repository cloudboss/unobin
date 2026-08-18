package runtime

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFinalizePlanFileV2ComputesDigest(t *testing.T) {
	draft := validPlanFileV2(t)
	draft.FormatVersion = 0
	draft.Digest = strings.Repeat("f", 64)

	plan, err := finalizePlanFileV2(draft)
	require.NoError(t, err)
	require.Equal(t, PlanFormatVersionV2, plan.FormatVersion)
	require.Len(t, plan.Digest, 64)
	require.NotEqual(t, draft.Digest, plan.Digest)
	require.NoError(t, plan.Validate())
}

func TestFinalizePlanFileV2CopiesEveryOperationKind(t *testing.T) {
	draft := validPlanFileV2(t)
	draft.FormatVersion = 0
	draft.Digest = ""
	operations := validStepOperations(t)
	draft.Steps = []PlanStepV2{
		{
			Address:   "resource.api",
			Kind:      NodeResource,
			DependsOn: []string{},
			Operation: operations[NodeResource],
		},
		{
			Address:   "action.notify",
			Kind:      NodeAction,
			DependsOn: []string{},
			Operation: operations[NodeAction],
		},
		{
			Address:   "data-source.image",
			Kind:      NodeDataSource,
			DependsOn: []string{},
			Operation: operations[NodeDataSource],
		},
		{
			Address:   "library-config.cloud",
			Kind:      NodeLibraryConfiguration,
			DependsOn: []string{},
			Operation: operations[NodeLibraryConfiguration],
		},
		{
			Address:   "output.url",
			Kind:      NodeOutput,
			DependsOn: []string{},
			Operation: operations[NodeOutput],
		},
	}
	compositePrior := operationCompositeState(t, NodeResource)
	draft.Steps = append(draft.Steps, PlanStepV2{
		Address:   "resource.application",
		Kind:      NodeResource,
		DependsOn: []string{},
		Operation: StepOperation{
			Kind: StepComposite,
			Composite: &CompositePlanOperation{
				Decision: DecisionDestroy,
				Prior:    &compositePrior,
			},
		},
	})

	plan, err := finalizePlanFileV2(draft)
	require.NoError(t, err)
	require.Len(t, plan.Steps, 6)
	require.NoError(t, plan.Validate())
}

func TestFinalizePlanFileV2CopiesMutableData(t *testing.T) {
	draft := validPlanFileV2(t)
	draft.FormatVersion = 0
	draft.Digest = ""
	draft.Backend = &StateRefV2{
		Name: "local",
		Body: operationObject(t, map[string]EncodedValue{}),
	}
	draft.StateMoves = []PlannedEntryMove{{
		From: "resource.old",
		To:   "resource.api",
	}}
	draft.Steps[0].DependsOn = []string{"resource.network"}
	expectedSensitivePaths := slices.Clone(
		draft.Steps[0].Operation.Resource.Desired.SensitiveInputPaths,
	)

	plan, err := finalizePlanFileV2(draft)
	require.NoError(t, err)

	draft.Backend.Name = "changed"
	draft.StateMoves[0].From = "resource.changed"
	draft.Steps[0].DependsOn[0] = "resource.changed"
	draft.Steps[0].Operation.Resource.Desired.SensitiveInputPaths = []string{"/changed"}

	require.Equal(t, "local", plan.Backend.Name)
	require.Equal(t, "resource.old", plan.StateMoves[0].From)
	require.Equal(t, []string{"resource.network"}, plan.Steps[0].DependsOn)
	require.Equal(
		t,
		expectedSensitivePaths,
		plan.Steps[0].Operation.Resource.Desired.SensitiveInputPaths,
	)
	require.NoError(t, plan.Validate())
}

func TestFinalizePlanFileV2RejectsInvalidPlan(t *testing.T) {
	draft := validPlanFileV2(t)
	draft.FormatVersion = 0
	draft.Digest = ""
	draft.Stack = ""

	plan, err := finalizePlanFileV2(draft)
	require.ErrorContains(t, err, "stack is required")
	require.Equal(t, PlanFileV2{}, plan)
}
