package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func newActionApplySnapshot(
	t *testing.T,
	actions map[string]ActionStatePayload,
) *state.SnapshotV2 {
	t.Helper()
	snapshot := newRegisteredApplySnapshot(t, nil)
	for address, action := range actions {
		require.NoError(t, snapshot.SetEntry(state.StateEntryV2{
			Address: address,
			Kind:    state.StateAction,
			Payload: state.StatePayload{
				Kind:   state.StateAction,
				Action: &action,
			},
		}))
	}
	return snapshot
}

func actionApplyStep(
	t *testing.T,
	address string,
	dependsOn []string,
	operation ActionPlanOperation,
) PlanStepV2 {
	t.Helper()
	step := PlanStepV2{
		Address:   address,
		Kind:      NodeAction,
		DependsOn: dependsOn,
		Operation: StepOperation{
			Kind:   StepAction,
			Action: &operation,
		},
	}
	require.NoError(t, step.Validate())
	return step
}

func TestApplyActionSnapshotStepRerunsAndPersistsState(t *testing.T) {
	desired := validPlannedActionTarget(t)
	outputs := operationObject(t, map[string]EncodedValue{
		"sent": BooleanValue(true),
	})
	step := actionApplyStep(
		t,
		"action.notify",
		[]string{"resource.service"},
		ActionPlanOperation{
			Decision: DecisionRerun,
			Desired:  &desired,
		},
	)
	original := newActionApplySnapshot(t, nil)
	var persisted *state.SnapshotV2
	applyState, err := newApplyStateV2(
		original,
		func(_ context.Context, snapshot *state.SnapshotV2) error {
			persisted = snapshot
			return nil
		},
	)
	require.NoError(t, err)
	generatedAt := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	applyState.now = func() time.Time { return generatedAt }
	runs := 0

	target, err := applyActionSnapshotStep(
		context.Background(),
		applyState,
		actionSnapshotApplyRequest{
			Step:    step,
			Desired: &desired,
			Run: func(context.Context) (EncodedValue, error) {
				runs++
				return outputs, nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, runs)
	require.Empty(t, original.Entries)
	require.NotNil(t, persisted)
	require.Equal(t, generatedAt, persisted.GeneratedAt)

	expected := ActionStatePayload{
		Binding:              desired.Binding,
		Inputs:               desired.Inputs,
		Outputs:              outputs,
		Configuration:        *desired.Configuration.Record,
		TriggerHash:          desired.TriggerHash,
		DependsOn:            []string{"resource.service"},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	require.Equal(t, &expected, target)
	entry := persisted.Find("action.notify")
	require.NotNil(t, entry)
	require.Equal(t, state.StateAction, entry.Kind)
	require.Equal(t, target, entry.Payload.Action)

	persisted.Entries[0].Payload.Action.DependsOn[0] = "resource.changed"
	current, err := applyState.snapshotCopy()
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"resource.service"},
		current.Find("action.notify").Payload.Action.DependsOn,
	)

	read, err := applyState.actionState("action.notify")
	require.NoError(t, err)
	read.DependsOn[0] = "resource.changed"
	readAgain, err := applyState.actionState("action.notify")
	require.NoError(t, err)
	require.Equal(t, []string{"resource.service"}, readAgain.DependsOn)
}

func TestApplyActionSnapshotStepSkipsWithoutRunning(t *testing.T) {
	desired := validPlannedActionTarget(t)
	desired.Inputs = operationObject(t, map[string]EncodedValue{
		"message": StringValue("updated"),
	})
	prior := operationActionState(t)
	step := actionApplyStep(
		t,
		"action.notify",
		[]string{"resource.service"},
		ActionPlanOperation{
			Decision: DecisionSkip,
			Desired:  &desired,
			Prior:    &prior,
		},
	)
	applyState, err := newApplyStateV2(
		newActionApplySnapshot(t, map[string]ActionStatePayload{
			"action.notify": prior,
		}),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)
	run := false

	target, err := applyActionSnapshotStep(
		context.Background(),
		applyState,
		actionSnapshotApplyRequest{
			Step:    step,
			Desired: &desired,
			Run: func(context.Context) (EncodedValue, error) {
				run = true
				return EncodedValue{}, nil
			},
		},
	)
	require.NoError(t, err)
	require.False(t, run)
	require.Equal(t, desired.Inputs, target.Inputs)
	require.Equal(t, prior.Outputs, target.Outputs)
	require.Equal(t, []string{"resource.service"}, target.DependsOn)
}

func TestApplyActionSnapshotStepDestroysStateWithoutRunning(t *testing.T) {
	prior := operationActionState(t)
	step := actionApplyStep(
		t,
		"action.notify",
		[]string{},
		ActionPlanOperation{
			Decision: DecisionDestroy,
			Prior:    &prior,
		},
	)
	applyState, err := newApplyStateV2(
		newActionApplySnapshot(t, map[string]ActionStatePayload{
			"action.notify": prior,
		}),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)
	run := false

	target, err := applyActionSnapshotStep(
		context.Background(),
		applyState,
		actionSnapshotApplyRequest{
			Step: step,
			Run: func(context.Context) (EncodedValue, error) {
				run = true
				return EncodedValue{}, nil
			},
		},
	)
	require.NoError(t, err)
	require.Nil(t, target)
	require.False(t, run)
	current, err := applyState.snapshotCopy()
	require.NoError(t, err)
	require.Nil(t, current.Find("action.notify"))
}

func TestApplyActionSnapshotStepRejectsPriorStateMismatch(t *testing.T) {
	desired := validPlannedActionTarget(t)
	plannedPrior := operationActionState(t)
	changedPrior := plannedPrior
	changedPrior.TriggerHash = "changed"
	tests := []struct {
		name      string
		operation ActionPlanOperation
		actions   map[string]ActionStatePayload
		message   string
	}{
		{
			name: "missing",
			operation: ActionPlanOperation{
				Decision: DecisionRerun,
				Desired:  &desired,
				Prior:    &plannedPrior,
			},
			message: "saved plan requires prior action state at action.notify",
		},
		{
			name: "changed",
			operation: ActionPlanOperation{
				Decision: DecisionRerun,
				Desired:  &desired,
				Prior:    &plannedPrior,
			},
			actions: map[string]ActionStatePayload{
				"action.notify": changedPrior,
			},
			message: "prior action state does not match the saved plan",
		},
		{
			name: "unexpected",
			operation: ActionPlanOperation{
				Decision: DecisionRerun,
				Desired:  &desired,
			},
			actions: map[string]ActionStatePayload{
				"action.notify": plannedPrior,
			},
			message: "saved plan forbids prior action state at action.notify",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			persisted := false
			applyState, err := newApplyStateV2(
				newActionApplySnapshot(t, test.actions),
				func(context.Context, *state.SnapshotV2) error {
					persisted = true
					return nil
				},
			)
			require.NoError(t, err)
			run := false

			target, err := applyActionSnapshotStep(
				context.Background(),
				applyState,
				actionSnapshotApplyRequest{
					Step: actionApplyStep(
						t,
						"action.notify",
						[]string{},
						test.operation,
					),
					Desired: &desired,
					Run: func(context.Context) (EncodedValue, error) {
						run = true
						return operationObject(t, map[string]EncodedValue{}), nil
					},
				},
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
			require.False(t, run)
			require.False(t, persisted)
		})
	}
}

func TestApplyActionSnapshotStepKeepsStateOnPersistenceFailure(t *testing.T) {
	desired := validPlannedActionTarget(t)
	step := actionApplyStep(
		t,
		"action.notify",
		[]string{},
		ActionPlanOperation{
			Decision: DecisionRerun,
			Desired:  &desired,
		},
	)
	expectedErr := errors.New("state write failed")
	applyState, err := newApplyStateV2(
		newActionApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error {
			return expectedErr
		},
	)
	require.NoError(t, err)
	before, err := applyState.snapshotCopy()
	require.NoError(t, err)
	run := false

	target, err := applyActionSnapshotStep(
		context.Background(),
		applyState,
		actionSnapshotApplyRequest{
			Step:    step,
			Desired: &desired,
			Run: func(context.Context) (EncodedValue, error) {
				run = true
				return operationObject(t, map[string]EncodedValue{}), nil
			},
		},
	)
	require.ErrorIs(t, err, expectedErr)
	require.Nil(t, target)
	require.True(t, run)
	after, copyErr := applyState.snapshotCopy()
	require.NoError(t, copyErr)
	require.Equal(t, before, after)
}

func TestApplyActionSnapshotStepRejectsInvalidSetup(t *testing.T) {
	desired := validPlannedActionTarget(t)
	step := actionApplyStep(
		t,
		"action.notify",
		[]string{},
		ActionPlanOperation{
			Decision: DecisionRerun,
			Desired:  &desired,
		},
	)
	applyState, err := newApplyStateV2(
		newActionApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)
	request := actionSnapshotApplyRequest{
		Step:    step,
		Desired: &desired,
		Run: func(context.Context) (EncodedValue, error) {
			return operationObject(t, map[string]EncodedValue{}), nil
		},
	}
	var nilContext context.Context

	target, err := applyActionSnapshotStep(nilContext, applyState, request)
	require.ErrorContains(t, err, "action apply context is required")
	require.Nil(t, target)
	target, err = applyActionSnapshotStep(context.Background(), nil, request)
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
	target, err = applyActionSnapshotStep(context.Background(), applyState, request)
	require.ErrorContains(t, err, "saved action step must be an action")
	require.Nil(t, target)
}

func TestApplyStateV2RejectsNonActionEntryForActionAddress(t *testing.T) {
	snapshot := newActionApplySnapshot(t, nil)
	composite := operationCompositeState(t, NodeAction)
	require.NoError(t, snapshot.SetEntry(state.StateEntryV2{
		Address: "action.notify",
		Kind:    state.StateComposite,
		Payload: state.StatePayload{
			Kind:      state.StateComposite,
			Composite: &composite,
		},
	}))
	applyState, err := newApplyStateV2(
		snapshot,
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)

	action, err := applyState.actionState("action.notify")
	require.ErrorContains(t, err, "state entry action.notify is not an action")
	require.Nil(t, action)
}
