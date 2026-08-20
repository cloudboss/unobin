package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestApplyPlanStepsV2DispatchesEveryOperationInDependencyOrder(t *testing.T) {
	steps := mixedApplyPlanStepsV2(t)
	var persisted *state.SnapshotV2
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(_ context.Context, snapshot *state.SnapshotV2) error {
			persisted = snapshot
			return nil
		},
	)
	require.NoError(t, err)

	var applied []string
	var states []*applyStateV2
	record := func(name string) planStepV2ApplyCallback {
		return func(
			_ context.Context,
			gotState *applyStateV2,
			step PlanStepV2,
		) error {
			applied = append(applied, name+":"+step.Address)
			states = append(states, gotState)
			return nil
		}
	}

	err = applyPlanStepsV2(
		context.Background(),
		applyState,
		steps,
		3,
		applyPlanStepsV2Callbacks{
			Resource:             record("resource"),
			Action:               record("action"),
			DataSource:           record("data-source"),
			LibraryConfiguration: record("library-configuration"),
			Composite:            record("composite"),
			Output: func(_ context.Context, step PlanStepV2) (EncodedValue, error) {
				applied = append(applied, "output:"+step.Address)
				return StringValue("ok"), nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{
		"library-configuration:library-config.cloud",
		"resource:resource.service",
		"data-source:data-source.lookup",
		"action:action.notify",
		"composite:resource.application",
		"output:output.result",
	}, applied)
	require.Len(t, states, 5)
	for _, gotState := range states {
		require.Same(t, applyState, gotState)
	}
	require.NotNil(t, persisted)
	expectedOutputs, err := ObjectValue(map[string]EncodedValue{
		"result": StringValue("ok"),
	})
	require.NoError(t, err)
	require.Equal(t, expectedOutputs, persisted.Outputs)
}

func TestApplyPlanStepsV2StopsBeforeDependentAndOutputWork(t *testing.T) {
	operations := validStepOperations(t)
	steps := []PlanStepV2{
		{
			Address:   "resource.service",
			Kind:      NodeResource,
			DependsOn: []string{},
			Operation: operations[NodeResource],
		},
		{
			Address:   "action.notify",
			Kind:      NodeAction,
			DependsOn: []string{"resource.service"},
			Operation: operations[NodeAction],
		},
		{
			Address:   "output.result",
			Kind:      NodeOutput,
			DependsOn: []string{"action.notify"},
			Operation: operations[NodeOutput],
		},
	}
	for i := range steps {
		require.NoError(t, steps[i].Validate())
	}

	persisted := false
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error {
			persisted = true
			return nil
		},
	)
	require.NoError(t, err)
	expectedErr := errors.New("resource failed")
	actionApplied := false
	outputEvaluated := false

	err = applyPlanStepsV2(
		context.Background(),
		applyState,
		steps,
		2,
		applyPlanStepsV2Callbacks{
			Resource: func(context.Context, *applyStateV2, PlanStepV2) error {
				return expectedErr
			},
			Action: func(context.Context, *applyStateV2, PlanStepV2) error {
				actionApplied = true
				return nil
			},
			Output: func(context.Context, PlanStepV2) (EncodedValue, error) {
				outputEvaluated = true
				return StringValue("complete"), nil
			},
		},
	)
	require.ErrorIs(t, err, expectedErr)
	require.ErrorContains(t, err, "resource.service")
	require.False(t, actionApplied)
	require.False(t, outputEvaluated)
	require.False(t, persisted)
}

func TestApplyPlanStepsV2ClearsOutputsForEmptyPlan(t *testing.T) {
	snapshot := newRegisteredApplySnapshot(t, nil)
	oldOutputs, err := ObjectValue(map[string]EncodedValue{
		"old": StringValue("value"),
	})
	require.NoError(t, err)
	require.NoError(t, snapshot.SetOutputs(oldOutputs, []string{}))
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

	err = applyPlanStepsV2(
		context.Background(),
		applyState,
		[]PlanStepV2{},
		1,
		applyPlanStepsV2Callbacks{
			Output: func(context.Context, PlanStepV2) (EncodedValue, error) {
				evaluated = true
				return EncodedValue{}, nil
			},
		},
	)
	require.NoError(t, err)
	require.False(t, evaluated)
	require.NotNil(t, persisted)
	emptyOutputs, err := ObjectValue(map[string]EncodedValue{})
	require.NoError(t, err)
	require.Equal(t, emptyOutputs, persisted.Outputs)
}

func TestApplyPlanStepsV2RejectsInvalidSetupBeforeApply(t *testing.T) {
	steps := mixedApplyPlanStepsV2(t)
	invalid := steps[1]
	invalid.DependsOn = nil
	cycleResource := steps[1]
	cycleResource.DependsOn = []string{"action.notify"}
	cycleAction := steps[3]
	cycleAction.DependsOn = []string{"resource.service"}

	tests := []struct {
		name       string
		ctx        context.Context
		missing    string
		steps      []PlanStepV2
		applyState bool
		message    string
	}{
		{
			name:       "missing context",
			steps:      []PlanStepV2{},
			applyState: true,
			message:    "apply context is required",
		},
		{
			name:    "missing apply state",
			ctx:     context.Background(),
			steps:   []PlanStepV2{},
			message: "version 2 apply state is required",
		},
		{
			name:       "missing output evaluator",
			ctx:        context.Background(),
			missing:    "output",
			steps:      []PlanStepV2{},
			applyState: true,
			message:    "output evaluator is required",
		},
		{
			name:       "missing resource callback",
			ctx:        context.Background(),
			missing:    "resource",
			steps:      []PlanStepV2{steps[1]},
			applyState: true,
			message:    "resource apply callback is required",
		},
		{
			name:       "missing action callback",
			ctx:        context.Background(),
			missing:    "action",
			steps:      []PlanStepV2{steps[3]},
			applyState: true,
			message:    "action apply callback is required",
		},
		{
			name:       "missing data-source callback",
			ctx:        context.Background(),
			missing:    "data-source",
			steps:      []PlanStepV2{steps[2]},
			applyState: true,
			message:    "data-source apply callback is required",
		},
		{
			name:       "missing library-configuration callback",
			ctx:        context.Background(),
			missing:    "library-configuration",
			steps:      []PlanStepV2{steps[0]},
			applyState: true,
			message:    "library-configuration apply callback is required",
		},
		{
			name:       "missing composite callback",
			ctx:        context.Background(),
			missing:    "composite",
			steps:      []PlanStepV2{steps[4]},
			applyState: true,
			message:    "composite apply callback is required",
		},
		{
			name:       "invalid step",
			ctx:        context.Background(),
			steps:      []PlanStepV2{invalid},
			applyState: true,
			message:    "step 0",
		},
		{
			name:       "dependency cycle",
			ctx:        context.Background(),
			steps:      []PlanStepV2{cycleResource, cycleAction},
			applyState: true,
			message:    "dependency cycle",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			applied := false
			persisted := false
			var applyState *applyStateV2
			if test.applyState {
				var err error
				applyState, err = newApplyStateV2(
					newRegisteredApplySnapshot(t, nil),
					func(context.Context, *state.SnapshotV2) error {
						persisted = true
						return nil
					},
				)
				require.NoError(t, err)
			}
			apply := func(context.Context, *applyStateV2, PlanStepV2) error {
				applied = true
				return nil
			}
			callbacks := applyPlanStepsV2Callbacks{
				Resource:             apply,
				Action:               apply,
				DataSource:           apply,
				LibraryConfiguration: apply,
				Composite:            apply,
				Output: func(context.Context, PlanStepV2) (EncodedValue, error) {
					applied = true
					return StringValue("complete"), nil
				},
			}
			switch test.missing {
			case "resource":
				callbacks.Resource = nil
			case "action":
				callbacks.Action = nil
			case "data-source":
				callbacks.DataSource = nil
			case "library-configuration":
				callbacks.LibraryConfiguration = nil
			case "composite":
				callbacks.Composite = nil
			case "output":
				callbacks.Output = nil
			}

			err := applyPlanStepsV2(
				test.ctx,
				applyState,
				test.steps,
				2,
				callbacks,
			)
			require.ErrorContains(t, err, test.message)
			require.False(t, applied)
			require.False(t, persisted)
		})
	}
}

func mixedApplyPlanStepsV2(t *testing.T) []PlanStepV2 {
	t.Helper()
	operations := validStepOperations(t)
	compositeDesired := validPlannedCompositeTarget(t, NodeResource)
	steps := []PlanStepV2{
		{
			Address:   "library-config.cloud",
			Kind:      NodeLibraryConfiguration,
			DependsOn: []string{},
			Operation: operations[NodeLibraryConfiguration],
		},
		{
			Address:   "resource.service",
			Kind:      NodeResource,
			DependsOn: []string{"library-config.cloud"},
			Operation: operations[NodeResource],
		},
		{
			Address:   "data-source.lookup",
			Kind:      NodeDataSource,
			DependsOn: []string{"resource.service"},
			Operation: operations[NodeDataSource],
		},
		{
			Address:   "action.notify",
			Kind:      NodeAction,
			DependsOn: []string{"data-source.lookup"},
			Operation: operations[NodeAction],
		},
		{
			Address:   "resource.application",
			Kind:      NodeResource,
			DependsOn: []string{"action.notify"},
			Operation: StepOperation{
				Kind: StepComposite,
				Composite: &CompositePlanOperation{
					Decision: DecisionEval,
					Desired:  &compositeDesired,
				},
			},
		},
		{
			Address:   "output.result",
			Kind:      NodeOutput,
			DependsOn: []string{"resource.application"},
			Operation: operations[NodeOutput],
		},
	}
	for i := range steps {
		require.NoError(t, steps[i].Validate())
	}
	return steps
}
