package runtime

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanFileV2Codec(t *testing.T) {
	plan := validPlanFileV2(t)

	encoded, err := EncodePlanV2(plan)
	require.NoError(t, err)
	require.True(t, bytes.HasSuffix(encoded, []byte{'\n'}))

	decoded, err := DecodePlanV2(encoded)
	require.NoError(t, err)
	require.Equal(t, plan, decoded)
}

func TestPlanFileV2CodecPreservesEveryOperationKind(t *testing.T) {
	addresses := map[NodeKind]string{
		NodeResource:             "resource.api",
		NodeAction:               "action.notify",
		NodeDataSource:           "data-source.image",
		NodeLibraryConfiguration: "library-config.cloud",
		NodeOutput:               "output.url",
	}
	steps := make([]PlanStepV2, 0, len(addresses)+1)
	for kind, operation := range validStepOperations(t) {
		steps = append(steps, PlanStepV2{
			Address:   addresses[kind],
			Kind:      kind,
			DependsOn: []string{},
			Operation: operation,
		})
	}
	compositePrior := operationCompositeState(t, NodeResource)
	steps = append(steps, PlanStepV2{
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

	for _, step := range steps {
		t.Run(string(step.Operation.Kind), func(t *testing.T) {
			plan := validPlanFileV2(t)
			plan.Steps = []PlanStepV2{step}
			plan.Digest = ""
			var err error
			plan.Digest, err = planFileV2Digest(plan)
			require.NoError(t, err)

			encoded, err := EncodePlanV2(plan)
			require.NoError(t, err)
			decoded, err := DecodePlanV2(encoded)
			require.NoError(t, err)
			require.Equal(t, plan, decoded)
		})
	}
}

func TestEncodePlanFileV2RejectsInvalidPlan(t *testing.T) {
	plan := validPlanFileV2(t)
	plan.Digest = strings.Repeat("f", 64)

	encoded, err := EncodePlanV2(plan)
	require.ErrorContains(t, err, "digest does not match plan contents")
	require.Nil(t, encoded)
}

func TestDecodePlanFileV2RejectsInvalidJSONContract(t *testing.T) {
	valid := marshalPlanFileV2(t, validPlanFileV2(t))
	tests := []struct {
		name    string
		old     string
		new     string
		message string
	}{
		{
			name:    "duplicate version member",
			old:     `"format-version":2,`,
			new:     `"format-version":2,"format-version":1,`,
			message: `$.format-version: duplicate member`,
		},
		{
			name:    "duplicate nested member",
			old:     `"factory":{"name":"deploy",`,
			new:     `"factory":{"name":"deploy","name":"again",`,
			message: `$.factory.name: duplicate member`,
		},
		{
			name:    "unknown nested member",
			old:     `"operation":{"kind":"resource",`,
			new:     `"operation":{"kind":"resource","unexpected":true,`,
			message: `$.steps[0].operation.unexpected: unknown member`,
		},
		{
			name:    "missing required member",
			old:     `"parallelism":4,`,
			new:     "",
			message: `$.parallelism: member is required`,
		},
		{
			name:    "missing nested required member",
			old:     `"depends-on":[],`,
			new:     "",
			message: `$.steps[0].depends-on: member is required`,
		},
		{
			name:    "null optional member",
			old:     `"parallelism":4,`,
			new:     `"backend":null,"parallelism":4,`,
			message: `$.backend: null is not allowed`,
		},
		{
			name:    "noncanonical integer",
			old:     `"parallelism":4,`,
			new:     `"parallelism":-0,`,
			message: `$.parallelism: noncanonical integer "-0"`,
		},
		{
			name:    "invalid version type",
			old:     `"format-version":2,`,
			new:     `"format-version":"2",`,
			message: `$.format-version: invalid value`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := replacePlanFileV2JSON(t, valid, tt.old, tt.new)
			plan, err := DecodePlanV2(input)
			require.ErrorContains(t, err, tt.message)
			require.Equal(t, PlanFileV2{}, plan)
		})
	}
}

func TestDecodePlanFileV2RejectsTrailingValue(t *testing.T) {
	input := append(marshalPlanFileV2(t, validPlanFileV2(t)), []byte(` {}`)...)

	plan, err := DecodePlanV2(input)
	require.ErrorContains(t, err, "$: unexpected value after JSON value")
	require.Equal(t, PlanFileV2{}, plan)
}

func TestDecodePlanFileV2RequiresFalseBooleanMembers(t *testing.T) {
	plan := validPlanFileV2(t)
	plan.Steps = []PlanStepV2{{
		Address:   "output.url",
		Kind:      NodeOutput,
		DependsOn: []string{},
		Operation: validStepOperations(t)[NodeOutput],
	}}
	plan.Digest = ""
	var err error
	plan.Digest, err = planFileV2Digest(plan)
	require.NoError(t, err)
	valid := marshalPlanFileV2(t, plan)
	input := replacePlanFileV2JSON(t, valid, `,"sensitive":false`, "")

	decoded, err := DecodePlanV2(input)
	require.ErrorContains(
		t,
		err,
		"$.steps[0].operation.output.sensitive: member is required",
	)
	require.Equal(t, PlanFileV2{}, decoded)
}

func TestDecodePlanFileV2RejectsObsoleteAlphaFormat(t *testing.T) {
	plan, err := DecodePlanV2([]byte(`{"format-version":1}`))
	require.ErrorContains(t, err, "obsolete alpha format; create a new plan or state")
	require.Equal(t, PlanFileV2{}, plan)
}

func TestDecodePlanFileV2VerifiesDigest(t *testing.T) {
	valid := marshalPlanFileV2(t, validPlanFileV2(t))
	input := replacePlanFileV2JSON(t, valid, `"stack":"production"`, `"stack":"other"`)

	plan, err := DecodePlanV2(input)
	require.ErrorContains(t, err, "digest does not match plan contents")
	require.Equal(t, PlanFileV2{}, plan)
}

func marshalPlanFileV2(t *testing.T, plan PlanFileV2) []byte {
	t.Helper()
	encoded, err := json.Marshal(plan)
	require.NoError(t, err)
	return encoded
}

func replacePlanFileV2JSON(t *testing.T, input []byte, old, new string) []byte {
	t.Helper()
	require.Contains(t, string(input), old)
	return bytes.Replace(input, []byte(old), []byte(new), 1)
}

func TestFinalizePlanFileV2ComputesDigest(t *testing.T) {
	draft := validPlanFileV2(t)
	draft.FormatVersion = 0
	draft.Digest = strings.Repeat("f", 64)

	plan, err := finalizePlanFileV2(draft)
	require.NoError(t, err)
	require.Equal(t, PlanFormatVersion, plan.FormatVersion)
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
