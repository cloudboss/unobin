package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestApplyOutputStepsPersistsResolvedValues(t *testing.T) {
	tokenPending, err := PendingEncodedValue([]string{"resource.service.token"})
	require.NoError(t, err)
	idPending, err := PendingEncodedValue([]string{"resource.service.id"})
	require.NoError(t, err)
	plannedMetadata, err := ObjectValue(map[string]EncodedValue{
		"id":     idPending,
		"region": StringValue("us-east-1"),
	})
	require.NoError(t, err)
	metadata, err := ObjectValue(map[string]EncodedValue{
		"id":     StringValue("service-1"),
		"region": StringValue("us-east-1"),
	})
	require.NoError(t, err)
	steps := []PlanStepV2{
		outputApplyStep(t, "output.token", tokenPending, true),
		outputApplyStep(t, "output.metadata", plannedMetadata, true),
		outputApplyStep(
			t,
			"output.endpoint",
			StringValue("https://example.com"),
			false,
		),
	}
	evaluatedValues := map[string]EncodedValue{
		"output.token":    StringValue("secret"),
		"output.metadata": metadata,
		"output.endpoint": StringValue("https://example.com"),
	}

	var persisted *state.SnapshotV2
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(_ context.Context, snapshot *state.SnapshotV2) error {
			persisted = snapshot
			return nil
		},
	)
	require.NoError(t, err)
	var evaluated []string

	err = applyOutputSteps(
		context.Background(),
		applyState,
		steps,
		func(_ context.Context, step PlanStepV2) (EncodedValue, error) {
			evaluated = append(evaluated, step.Address)
			return evaluatedValues[step.Address], nil
		},
	)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"output.token", "output.metadata", "output.endpoint"},
		evaluated,
	)
	require.NotNil(t, persisted)
	expectedOutputs, err := ObjectValue(map[string]EncodedValue{
		"token":    StringValue("secret"),
		"metadata": metadata,
		"endpoint": StringValue("https://example.com"),
	})
	require.NoError(t, err)
	require.Equal(t, expectedOutputs, persisted.Outputs)
	require.Equal(t, []string{"/metadata", "/token"}, persisted.SensitivePaths)

	current, err := applyState.snapshotCopy()
	require.NoError(t, err)
	require.Equal(t, persisted, current)
}

func TestApplyOutputStepsKeepsSnapshotOnEvaluationFailure(t *testing.T) {
	pending, err := PendingEncodedValue([]string{"resource.service.id"})
	require.NoError(t, err)
	expectedErr := errors.New("evaluation failed")
	tests := []struct {
		name        string
		planned     EncodedValue
		current     EncodedValue
		evaluateErr error
		message     string
	}{
		{
			name:    "known value changed",
			planned: StringValue("planned"),
			current: StringValue("current"),
			message: "changed since the plan was computed",
		},
		{
			name:    "value remains pending",
			planned: pending,
			current: pending,
			message: "must be concrete",
		},
		{
			name:        "evaluator failed",
			planned:     StringValue("planned"),
			evaluateErr: expectedErr,
			message:     "evaluate output output.result",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := newRegisteredApplySnapshot(t, nil)
			oldOutputs, objectErr := ObjectValue(map[string]EncodedValue{
				"old": StringValue("value"),
			})
			require.NoError(t, objectErr)
			require.NoError(t, snapshot.SetOutputs(oldOutputs, []string{}))
			persisted := false
			applyState, stateErr := newApplyStateV2(
				snapshot,
				func(context.Context, *state.SnapshotV2) error {
					persisted = true
					return nil
				},
			)
			require.NoError(t, stateErr)
			before, copyErr := applyState.snapshotCopy()
			require.NoError(t, copyErr)

			err := applyOutputSteps(
				context.Background(),
				applyState,
				[]PlanStepV2{
					outputApplyStep(t, "output.result", test.planned, false),
				},
				func(context.Context, PlanStepV2) (EncodedValue, error) {
					return test.current, test.evaluateErr
				},
			)
			require.ErrorContains(t, err, test.message)
			if test.evaluateErr != nil {
				require.ErrorIs(t, err, expectedErr)
			}
			require.False(t, persisted)
			after, copyErr := applyState.snapshotCopy()
			require.NoError(t, copyErr)
			require.Equal(t, before, after)
		})
	}
}

func TestApplyOutputStepsClearsOutputsWhenNoStepsRemain(t *testing.T) {
	snapshot := newRegisteredApplySnapshot(t, nil)
	oldOutputs, err := ObjectValue(map[string]EncodedValue{
		"token": StringValue("secret"),
	})
	require.NoError(t, err)
	require.NoError(t, snapshot.SetOutputs(oldOutputs, []string{"/token"}))
	var persisted *state.SnapshotV2
	applyState, err := newApplyStateV2(
		snapshot,
		func(_ context.Context, next *state.SnapshotV2) error {
			persisted = next
			return nil
		},
	)
	require.NoError(t, err)
	evaluated := false

	err = applyOutputSteps(
		context.Background(),
		applyState,
		[]PlanStepV2{},
		func(context.Context, PlanStepV2) (EncodedValue, error) {
			evaluated = true
			return EncodedValue{}, nil
		},
	)
	require.NoError(t, err)
	require.False(t, evaluated)
	require.NotNil(t, persisted)
	emptyOutputs, err := ObjectValue(map[string]EncodedValue{})
	require.NoError(t, err)
	require.Equal(t, emptyOutputs, persisted.Outputs)
	require.Empty(t, persisted.SensitivePaths)
}

func TestApplyOutputStepsRejectsInvalidSetupBeforeEvaluation(t *testing.T) {
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)
	valid := outputApplyStep(t, "output.result", StringValue("value"), false)
	invalid := valid
	invalid.DependsOn = nil
	resource := applyScheduleV2ResourceStep(
		t,
		"resource.service",
		[]string{},
		DecisionCreate,
	)
	evaluate := func(context.Context, PlanStepV2) (EncodedValue, error) {
		return StringValue("value"), nil
	}
	tests := []struct {
		name       string
		ctx        context.Context
		applyState *applyStateV2
		steps      []PlanStepV2
		evaluate   outputStepEvaluator
		message    string
	}{
		{
			name:       "missing context",
			applyState: applyState,
			steps:      []PlanStepV2{},
			evaluate:   evaluate,
			message:    "apply context is required",
		},
		{
			name:     "missing apply state",
			ctx:      context.Background(),
			steps:    []PlanStepV2{},
			evaluate: evaluate,
			message:  "version 2 apply state is required",
		},
		{
			name:       "missing evaluator",
			ctx:        context.Background(),
			applyState: applyState,
			steps:      []PlanStepV2{},
			message:    "output evaluator is required",
		},
		{
			name:       "invalid output step",
			ctx:        context.Background(),
			applyState: applyState,
			steps:      []PlanStepV2{valid, invalid},
			evaluate:   evaluate,
			message:    "step 1",
		},
		{
			name:       "non-output step",
			ctx:        context.Background(),
			applyState: applyState,
			steps:      []PlanStepV2{valid, resource},
			evaluate:   evaluate,
			message:    "step 1 must be an output",
		},
		{
			name:       "duplicate output",
			ctx:        context.Background(),
			applyState: applyState,
			steps:      []PlanStepV2{valid, valid},
			evaluate:   evaluate,
			message:    "duplicate output address",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evaluated := false
			var evaluator outputStepEvaluator
			if test.evaluate != nil {
				evaluator = func(
					ctx context.Context,
					step PlanStepV2,
				) (EncodedValue, error) {
					evaluated = true
					return test.evaluate(ctx, step)
				}
			}
			err := applyOutputSteps(
				test.ctx,
				test.applyState,
				test.steps,
				evaluator,
			)
			require.ErrorContains(t, err, test.message)
			require.False(t, evaluated)
		})
	}
}

func TestApplyOutputStepsStopsForCanceledContext(t *testing.T) {
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	evaluated := false

	err = applyOutputSteps(
		ctx,
		applyState,
		[]PlanStepV2{
			outputApplyStep(t, "output.result", StringValue("value"), false),
		},
		func(context.Context, PlanStepV2) (EncodedValue, error) {
			evaluated = true
			return StringValue("value"), nil
		},
	)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, evaluated)
}

func outputApplyStep(
	t *testing.T,
	address string,
	value EncodedValue,
	sensitive bool,
) PlanStepV2 {
	t.Helper()
	step := PlanStepV2{
		Address:   address,
		Kind:      NodeOutput,
		DependsOn: []string{},
		Operation: StepOperation{
			Kind: StepOutput,
			Output: &OutputPlanOperation{
				Decision:  DecisionEval,
				Value:     value,
				Sensitive: sensitive,
			},
		},
	}
	require.NoError(t, step.Validate())
	return step
}
