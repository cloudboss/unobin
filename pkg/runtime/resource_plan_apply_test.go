package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestApplyRegisteredResourceStepsPersistsInDependencyOrder(t *testing.T) {
	capture := &registeredApplyCapture{
		createResult: &registeredApplyOutput{ID: "object-1", Value: "created"},
	}
	definition := registeredApplyDefinition(1, nil)
	registration := newRegisteredApplyResource(t, definition, capture)
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
	desiredByAddress := make(map[string]PlannedResourceTarget)
	steps := make([]PlanStepV2, 0, 2)
	for _, resource := range []struct {
		address   string
		name      string
		dependsOn []string
	}{
		{address: "resource.network", name: "network", dependsOn: []string{}},
		{
			address:   "resource.app",
			name:      "app",
			dependsOn: []string{"resource.network"},
		},
	} {
		desired := registeredApplyDesired(
			t,
			registration,
			binding,
			configuration,
			registeredApplyInput{Name: resource.name, Size: 1},
		)
		operation, err := registration.planResourceOperation(
			resourcePlanningRequest{Desired: &desired},
		)
		require.NoError(t, err)
		desiredByAddress[resource.address] = desired
		steps = append(
			steps,
			registeredApplyStep(t, resource.address, resource.dependsOn, operation),
		)
	}

	var persisted [][]string
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(_ context.Context, snapshot *state.SnapshotV2) error {
			persisted = append(persisted, registeredResourceSnapshotAddresses(snapshot))
			return nil
		},
	)
	require.NoError(t, err)
	var evaluated []string

	err = applyRegisteredResourceSteps(
		context.Background(),
		applyState,
		steps,
		1,
		func(
			_ context.Context,
			step PlanStepV2,
		) (registeredResourceSnapshotApplyRequest, error) {
			evaluated = append(evaluated, step.Address)
			desired := desiredByAddress[step.Address]
			return registeredResourceSnapshotApplyRequest{
				Desired:             &desired,
				DesiredConfigType:   configurationDefinition,
				DesiredRegistration: registration,
			}, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"resource.network", "resource.app"}, evaluated)
	require.Equal(t, [][]string{
		{"resource.network"},
		{"resource.app", "resource.network"},
	}, persisted)

	current, err := applyState.snapshotCopy()
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"resource.app", "resource.network"},
		registeredResourceSnapshotAddresses(current),
	)
}

func registeredResourceSnapshotAddresses(snapshot *state.SnapshotV2) []string {
	addresses := make([]string, len(snapshot.Entries))
	for i := range snapshot.Entries {
		addresses[i] = snapshot.Entries[i].Address
	}
	return addresses
}

func TestApplyRegisteredResourceStepsStopsAfterEvaluationFailure(t *testing.T) {
	steps := []PlanStepV2{
		applyScheduleV2ResourceStep(t, "resource.a", []string{}, DecisionCreate),
		applyScheduleV2ResourceStep(t, "resource.b", []string{}, DecisionCreate),
	}
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)
	expectedErr := errors.New("evaluation failed")
	var evaluated []string

	err = applyRegisteredResourceSteps(
		context.Background(),
		applyState,
		steps,
		1,
		func(
			_ context.Context,
			step PlanStepV2,
		) (registeredResourceSnapshotApplyRequest, error) {
			evaluated = append(evaluated, step.Address)
			return registeredResourceSnapshotApplyRequest{}, expectedErr
		},
	)
	require.ErrorIs(t, err, expectedErr)
	require.Equal(t, []string{"resource.a"}, evaluated)
}

func TestApplyRegisteredResourceStepsRejectsOtherStepKindsBeforeEvaluation(t *testing.T) {
	desired := validPlannedActionTarget(t)
	action := PlanStepV2{
		Address:   "action.notify",
		Kind:      NodeAction,
		DependsOn: []string{},
		Operation: StepOperation{
			Kind: StepAction,
			Action: &ActionPlanOperation{
				Decision: DecisionRerun,
				Desired:  &desired,
			},
		},
	}
	require.NoError(t, action.Validate())
	compositeDesired := PlannedCompositeTarget{
		Category: NodeResource,
		Binding: Binding{
			LibraryPath: "example.com/composites",
			Export:      "application",
		},
		Inputs:               operationObject(t, nil),
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	composite := PlanStepV2{
		Address:   "resource.application",
		Kind:      NodeResource,
		DependsOn: []string{},
		Operation: StepOperation{
			Kind: StepComposite,
			Composite: &CompositePlanOperation{
				Decision: DecisionEval,
				Desired:  &compositeDesired,
			},
		},
	}
	require.NoError(t, composite.Validate())
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)

	for _, test := range []struct {
		name string
		step PlanStepV2
	}{
		{name: "action", step: action},
		{name: "composite resource", step: composite},
	} {
		t.Run(test.name, func(t *testing.T) {
			evaluated := false
			err := applyRegisteredResourceSteps(
				context.Background(),
				applyState,
				[]PlanStepV2{test.step},
				1,
				func(
					context.Context,
					PlanStepV2,
				) (registeredResourceSnapshotApplyRequest, error) {
					evaluated = true
					return registeredResourceSnapshotApplyRequest{}, nil
				},
			)
			require.ErrorContains(t, err, "step 0 must be a registered resource")
			require.False(t, evaluated)
		})
	}
}

func TestApplyRegisteredResourceStepsRejectsInvalidSetup(t *testing.T) {
	applyState, err := newApplyStateV2(
		newRegisteredApplySnapshot(t, nil),
		func(context.Context, *state.SnapshotV2) error { return nil },
	)
	require.NoError(t, err)
	evaluate := func(
		context.Context,
		PlanStepV2,
	) (registeredResourceSnapshotApplyRequest, error) {
		return registeredResourceSnapshotApplyRequest{}, nil
	}
	tests := []struct {
		name       string
		ctx        context.Context
		applyState *applyStateV2
		evaluate   registeredResourceStepEvaluator
		message    string
	}{
		{
			name:       "missing context",
			applyState: applyState,
			evaluate:   evaluate,
			message:    "apply context is required",
		},
		{
			name:     "missing apply state",
			ctx:      context.Background(),
			evaluate: evaluate,
			message:  "version 2 apply state is required",
		},
		{
			name:       "missing evaluator",
			ctx:        context.Background(),
			applyState: applyState,
			message:    "resource apply evaluator is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := applyRegisteredResourceSteps(
				test.ctx,
				test.applyState,
				[]PlanStepV2{},
				1,
				test.evaluate,
			)
			require.ErrorContains(t, err, test.message)
		})
	}
}
