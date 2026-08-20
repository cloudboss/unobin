package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestApplyPlanFileV2AppliesMovesBeforeSteps(t *testing.T) {
	target := validOperationResourceTarget(t)
	target.DependsOn = []string{}
	snapshot := newRegisteredApplySnapshot(t, map[string]ResourceTarget{
		"resource.old": target,
	})
	oldOutputs, err := ObjectValue(map[string]EncodedValue{
		"old": StringValue("value"),
	})
	require.NoError(t, err)
	require.NoError(t, snapshot.SetOutputs(oldOutputs, []string{}))

	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	var persisted []*state.SnapshotV2
	applyState, err := newApplyStateV2(
		snapshot,
		func(_ context.Context, next *state.SnapshotV2) error {
			persisted = append(persisted, next)
			return nil
		},
	)
	require.NoError(t, err)

	resourceApplied := false
	var stepAddress string
	var snapshotAtStep *state.SnapshotV2
	err = applyPlanFileV2(
		context.Background(),
		applyState,
		plan,
		applyPlanFileV2Callbacks(func(
			_ context.Context,
			current *applyStateV2,
			step PlanStepV2,
		) error {
			resourceApplied = true
			stepAddress = step.Address
			var copyErr error
			snapshotAtStep, copyErr = current.snapshotCopy()
			return copyErr
		}),
	)
	require.NoError(t, err)
	require.True(t, resourceApplied)
	require.Equal(t, "resource.api", stepAddress)
	require.NotNil(t, snapshotAtStep)
	require.Nil(t, snapshotAtStep.Find("resource.old"))
	require.NotNil(t, snapshotAtStep.Find("resource.api"))
	require.Len(t, persisted, 2)
	require.Nil(t, persisted[0].Find("resource.old"))
	require.NotNil(t, persisted[0].Find("resource.api"))
	require.Equal(t, oldOutputs, persisted[0].Outputs)
	emptyOutputs, err := ObjectValue(map[string]EncodedValue{})
	require.NoError(t, err)
	require.Equal(t, emptyOutputs, persisted[1].Outputs)
}

func TestApplyPlanFileV2RejectsSetupBeforeStateMoves(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*PlanFileV2, *applyPlanStepsV2Callbacks)
		message   string
		makeCycle bool
	}{
		{
			name: "invalid plan",
			change: func(plan *PlanFileV2, _ *applyPlanStepsV2Callbacks) {
				plan.Digest = "bad"
			},
			message: "saved plan: digest",
		},
		{
			name: "missing callback",
			change: func(_ *PlanFileV2, callbacks *applyPlanStepsV2Callbacks) {
				callbacks.Resource = nil
			},
			message: "resource apply callback is required",
		},
		{
			name:      "dependency cycle",
			message:   "dependency cycle",
			makeCycle: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
			if test.makeCycle {
				plan.Steps[0].DependsOn = []string{"resource.other"}
				other := plan.Steps[0]
				other.Address = "resource.other"
				other.DependsOn = []string{"resource.api"}
				plan.Steps = append(plan.Steps, other)
				plan = finalizeApplyPlanFileV2(t, plan)
			}

			applied := false
			callbacks := applyPlanFileV2Callbacks(func(
				context.Context,
				*applyStateV2,
				PlanStepV2,
			) error {
				applied = true
				return nil
			})
			if test.change != nil {
				test.change(&plan, &callbacks)
			}

			persisted := false
			applyState, err := newApplyStateV2(
				applyPlanFileV2Snapshot(t),
				func(context.Context, *state.SnapshotV2) error {
					persisted = true
					return nil
				},
			)
			require.NoError(t, err)

			err = applyPlanFileV2(
				context.Background(),
				applyState,
				plan,
				callbacks,
			)
			require.ErrorContains(t, err, test.message)
			require.False(t, applied)
			require.False(t, persisted)
			current, copyErr := applyState.snapshotCopy()
			require.NoError(t, copyErr)
			require.NotNil(t, current.Find("resource.old"))
			require.Nil(t, current.Find("resource.api"))
		})
	}
}

func TestApplyPlanFileV2RejectsMissingContextAndState(t *testing.T) {
	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	callbacks := applyPlanFileV2Callbacks(func(
		context.Context,
		*applyStateV2,
		PlanStepV2,
	) error {
		return nil
	})
	applyState, err := newApplyStateV2(
		applyPlanFileV2Snapshot(t),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)

	var missingContext context.Context
	err = applyPlanFileV2(missingContext, applyState, plan, callbacks)
	require.ErrorContains(t, err, "apply context is required")

	err = applyPlanFileV2(context.Background(), nil, plan, callbacks)
	require.ErrorContains(t, err, "version 2 apply state is required")
}

func TestApplyPlanFileV2StopsStepsWhenStateMoveFails(t *testing.T) {
	plan := applyPlanFileV2Plan(t, "resource.missing", "resource.api")
	persisted := false
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error {
			persisted = true
			return nil
		},
	)
	require.NoError(t, err)
	applied := false

	err = applyPlanFileV2(
		context.Background(),
		applyState,
		plan,
		applyPlanFileV2Callbacks(func(
			context.Context,
			*applyStateV2,
			PlanStepV2,
		) error {
			applied = true
			return nil
		}),
	)
	require.ErrorContains(t, err, "no state entry at resource.missing")
	require.False(t, applied)
	require.False(t, persisted)
}

func TestApplyPlanFileV2KeepsPersistedMoveAfterStepFailure(t *testing.T) {
	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	var persisted []*state.SnapshotV2
	applyState, err := newApplyStateV2(
		applyPlanFileV2Snapshot(t),
		func(_ context.Context, next *state.SnapshotV2) error {
			persisted = append(persisted, next)
			return nil
		},
	)
	require.NoError(t, err)
	expectedErr := errors.New("resource failed")

	err = applyPlanFileV2(
		context.Background(),
		applyState,
		plan,
		applyPlanFileV2Callbacks(func(
			context.Context,
			*applyStateV2,
			PlanStepV2,
		) error {
			return expectedErr
		}),
	)
	require.ErrorIs(t, err, expectedErr)
	require.ErrorContains(t, err, "resource.api")
	require.Len(t, persisted, 1)
	require.Nil(t, persisted[0].Find("resource.old"))
	require.NotNil(t, persisted[0].Find("resource.api"))
	current, copyErr := applyState.snapshotCopy()
	require.NoError(t, copyErr)
	require.Equal(t, persisted[0], current)
}

func applyPlanFileV2Plan(t *testing.T, from, to string) PlanFileV2 {
	t.Helper()
	operation := validResourcePlanOperation(t, DecisionNoOp)
	operation.Prior.DependsOn = []string{}
	plan := validPlanFileV2(t)
	plan.StateMoves = []PlannedEntryMove{{From: from, To: to}}
	plan.Steps = []PlanStepV2{{
		Address:   to,
		Kind:      NodeResource,
		DependsOn: []string{},
		Operation: StepOperation{
			Kind:     StepResource,
			Resource: &operation,
		},
	}}
	plan.Parallelism = 1
	return finalizeApplyPlanFileV2(t, plan)
}

func finalizeApplyPlanFileV2(t *testing.T, plan PlanFileV2) PlanFileV2 {
	t.Helper()
	finalized, err := finalizePlanFileV2(plan)
	require.NoError(t, err)
	return finalized
}

func applyPlanFileV2Snapshot(t *testing.T) *state.SnapshotV2 {
	t.Helper()
	target := validOperationResourceTarget(t)
	target.DependsOn = []string{}
	return newRegisteredApplySnapshot(t, map[string]ResourceTarget{
		"resource.old": target,
	})
}

func applyPlanFileV2Callbacks(
	resource planStepV2ApplyCallback,
) applyPlanStepsV2Callbacks {
	return applyPlanStepsV2Callbacks{
		Resource: resource,
		Output: func(context.Context, PlanStepV2) (EncodedValue, error) {
			return EncodedValue{}, errors.New("unexpected output evaluation")
		},
	}
}
