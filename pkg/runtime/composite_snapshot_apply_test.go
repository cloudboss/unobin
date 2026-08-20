package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func newCompositeApplySnapshot(
	t *testing.T,
	composites map[string]CompositeStatePayload,
) *state.SnapshotV2 {
	t.Helper()
	snapshot := newRegisteredApplySnapshot(t, nil)
	for address, composite := range composites {
		require.NoError(t, snapshot.SetEntry(state.StateEntryV2{
			Address: address,
			Kind:    state.StateComposite,
			Payload: state.StatePayload{
				Kind:      state.StateComposite,
				Composite: &composite,
			},
		}))
	}
	return snapshot
}

func compositeApplyStep(
	t *testing.T,
	address string,
	category NodeKind,
	dependsOn []string,
	operation CompositePlanOperation,
) PlanStepV2 {
	t.Helper()
	step := PlanStepV2{
		Address:   address,
		Kind:      category,
		DependsOn: dependsOn,
		Operation: StepOperation{
			Kind:      StepComposite,
			Composite: &operation,
		},
	}
	require.NoError(t, step.Validate())
	return step
}

func TestApplyCompositeSnapshotStepEvaluatesAndPersistsState(t *testing.T) {
	desired := validPlannedCompositeTarget(t, NodeResource)
	outputs := operationObject(t, map[string]EncodedValue{
		"url": StringValue("https://example.com"),
	})
	step := compositeApplyStep(
		t,
		"resource.application",
		NodeResource,
		[]string{"resource.network"},
		CompositePlanOperation{
			Decision: DecisionEval,
			Desired:  &desired,
		},
	)
	original := newCompositeApplySnapshot(t, nil)
	var persisted *state.SnapshotV2
	applyState, err := newApplyStateV2(
		original,
		func(_ context.Context, snapshot *state.SnapshotV2) error {
			persisted = snapshot
			return nil
		},
	)
	require.NoError(t, err)
	generatedAt := time.Date(2026, time.August, 20, 2, 0, 0, 0, time.UTC)
	applyState.now = func() time.Time { return generatedAt }
	evaluations := 0

	target, err := applyCompositeSnapshotStep(
		context.Background(),
		applyState,
		compositeSnapshotApplyRequest{
			Step:    step,
			Desired: &desired,
			Eval: func(context.Context) (EncodedValue, error) {
				evaluations++
				return outputs, nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, evaluations)
	require.Empty(t, original.Entries)
	require.NotNil(t, persisted)
	require.Equal(t, generatedAt, persisted.GeneratedAt)

	expected := CompositeStatePayload{
		Category:             string(NodeResource),
		Binding:              desired.Binding,
		Inputs:               desired.Inputs,
		Outputs:              outputs,
		DependsOn:            []string{"resource.network"},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	require.Equal(t, &expected, target)
	entry := persisted.Find("resource.application")
	require.NotNil(t, entry)
	require.Equal(t, state.StateComposite, entry.Kind)
	require.Equal(t, target, entry.Payload.Composite)

	persisted.Entries[0].Payload.Composite.DependsOn[0] = "resource.changed"
	current, err := applyState.snapshotCopy()
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"resource.network"},
		current.Find("resource.application").Payload.Composite.DependsOn,
	)

	read, err := applyState.compositeState("resource.application")
	require.NoError(t, err)
	read.DependsOn[0] = "resource.changed"
	readAgain, err := applyState.compositeState("resource.application")
	require.NoError(t, err)
	require.Equal(t, []string{"resource.network"}, readAgain.DependsOn)
}

func TestApplyCompositeSnapshotStepDestroysStateWithoutEvaluation(t *testing.T) {
	prior := operationCompositeState(t, NodeAction)
	step := compositeApplyStep(
		t,
		"action.release",
		NodeAction,
		[]string{},
		CompositePlanOperation{
			Decision: DecisionDestroy,
			Prior:    &prior,
		},
	)
	applyState, err := newApplyStateV2(
		newCompositeApplySnapshot(t, map[string]CompositeStatePayload{
			"action.release": prior,
		}),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)
	evaluated := false

	target, err := applyCompositeSnapshotStep(
		context.Background(),
		applyState,
		compositeSnapshotApplyRequest{
			Step: step,
			Eval: func(context.Context) (EncodedValue, error) {
				evaluated = true
				return EncodedValue{}, nil
			},
		},
	)
	require.NoError(t, err)
	require.Nil(t, target)
	require.False(t, evaluated)
	current, err := applyState.snapshotCopy()
	require.NoError(t, err)
	require.Nil(t, current.Find("action.release"))
}

func TestApplyCompositeSnapshotStepRejectsPriorStateMismatch(t *testing.T) {
	desired := validPlannedCompositeTarget(t, NodeResource)
	plannedPrior := operationCompositeState(t, NodeResource)
	changedPrior := plannedPrior
	changedPrior.Outputs = operationObject(t, map[string]EncodedValue{
		"url": StringValue("https://changed.example.com"),
	})
	tests := []struct {
		name       string
		operation  CompositePlanOperation
		composites map[string]CompositeStatePayload
		message    string
	}{
		{
			name: "missing",
			operation: CompositePlanOperation{
				Decision: DecisionEval,
				Desired:  &desired,
				Prior:    &plannedPrior,
			},
			message: "saved plan requires prior composite state at resource.application",
		},
		{
			name: "changed",
			operation: CompositePlanOperation{
				Decision: DecisionEval,
				Desired:  &desired,
				Prior:    &plannedPrior,
			},
			composites: map[string]CompositeStatePayload{
				"resource.application": changedPrior,
			},
			message: "prior composite state does not match the saved plan",
		},
		{
			name: "unexpected",
			operation: CompositePlanOperation{
				Decision: DecisionEval,
				Desired:  &desired,
			},
			composites: map[string]CompositeStatePayload{
				"resource.application": plannedPrior,
			},
			message: "saved plan forbids prior composite state at resource.application",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			persisted := false
			applyState, err := newApplyStateV2(
				newCompositeApplySnapshot(t, test.composites),
				func(context.Context, *state.SnapshotV2) error {
					persisted = true
					return nil
				},
			)
			require.NoError(t, err)
			evaluated := false

			target, err := applyCompositeSnapshotStep(
				context.Background(),
				applyState,
				compositeSnapshotApplyRequest{
					Step: compositeApplyStep(
						t,
						"resource.application",
						NodeResource,
						[]string{},
						test.operation,
					),
					Desired: &desired,
					Eval: func(context.Context) (EncodedValue, error) {
						evaluated = true
						return operationObject(t, map[string]EncodedValue{}), nil
					},
				},
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
			require.False(t, evaluated)
			require.False(t, persisted)
		})
	}
}

func TestApplyCompositeSnapshotStepKeepsStateOnPersistenceFailure(t *testing.T) {
	desired := validPlannedCompositeTarget(t, NodeResource)
	step := compositeApplyStep(
		t,
		"resource.application",
		NodeResource,
		[]string{},
		CompositePlanOperation{
			Decision: DecisionEval,
			Desired:  &desired,
		},
	)
	expectedErr := errors.New("state write failed")
	applyState, err := newApplyStateV2(
		newCompositeApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error {
			return expectedErr
		},
	)
	require.NoError(t, err)
	before, err := applyState.snapshotCopy()
	require.NoError(t, err)
	evaluated := false

	target, err := applyCompositeSnapshotStep(
		context.Background(),
		applyState,
		compositeSnapshotApplyRequest{
			Step:    step,
			Desired: &desired,
			Eval: func(context.Context) (EncodedValue, error) {
				evaluated = true
				return operationObject(t, map[string]EncodedValue{}), nil
			},
		},
	)
	require.ErrorIs(t, err, expectedErr)
	require.Nil(t, target)
	require.True(t, evaluated)
	after, copyErr := applyState.snapshotCopy()
	require.NoError(t, copyErr)
	require.Equal(t, before, after)
}

func TestApplyCompositeSnapshotStepRejectsInvalidSetup(t *testing.T) {
	desired := validPlannedCompositeTarget(t, NodeResource)
	step := compositeApplyStep(
		t,
		"resource.application",
		NodeResource,
		[]string{},
		CompositePlanOperation{
			Decision: DecisionEval,
			Desired:  &desired,
		},
	)
	applyState, err := newApplyStateV2(
		newCompositeApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)
	request := compositeSnapshotApplyRequest{
		Step:    step,
		Desired: &desired,
		Eval: func(context.Context) (EncodedValue, error) {
			return operationObject(t, map[string]EncodedValue{}), nil
		},
	}
	var nilContext context.Context

	target, err := applyCompositeSnapshotStep(nilContext, applyState, request)
	require.ErrorContains(t, err, "composite apply context is required")
	require.Nil(t, target)
	target, err = applyCompositeSnapshotStep(context.Background(), nil, request)
	require.ErrorContains(t, err, "version 2 apply state is required")
	require.Nil(t, target)
	output := OutputPlanOperation{
		Decision: DecisionEval,
		Value:    StringValue("value"),
	}
	request.Step = PlanStepV2{
		Address:   "output.result",
		Kind:      NodeOutput,
		DependsOn: []string{},
		Operation: StepOperation{
			Kind:   StepOutput,
			Output: &output,
		},
	}
	require.NoError(t, request.Step.Validate())
	target, err = applyCompositeSnapshotStep(context.Background(), applyState, request)
	require.ErrorContains(t, err, "saved composite step must be a composite")
	require.Nil(t, target)
}

func TestApplyStateV2RejectsNonCompositeEntryForCompositeAddress(t *testing.T) {
	snapshot := newRegisteredApplySnapshot(t, map[string]ResourceTarget{
		"resource.application": validOperationResourceTarget(t),
	})
	applyState, err := newApplyStateV2(
		snapshot,
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)

	composite, err := applyState.compositeState("resource.application")
	require.ErrorContains(t, err, "state entry resource.application is not a composite")
	require.Nil(t, composite)
}
