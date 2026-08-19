package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunApplyScheduleV2OrdersCreateAndDestroySteps(t *testing.T) {
	tests := []struct {
		name     string
		steps    []PlanStepV2
		expected []string
	}{
		{
			name: "create follows dependencies",
			steps: []PlanStepV2{
				applyScheduleV2ResourceStep(
					t,
					"resource.app",
					[]string{"resource.network"},
					DecisionCreate,
				),
				applyScheduleV2ResourceStep(
					t,
					"resource.network",
					[]string{},
					DecisionCreate,
				),
			},
			expected: []string{"resource.network", "resource.app"},
		},
		{
			name: "destroy reverses dependencies",
			steps: []PlanStepV2{
				applyScheduleV2ResourceStep(
					t,
					"resource.network",
					[]string{},
					DecisionDestroy,
				),
				applyScheduleV2ResourceStep(
					t,
					"resource.app",
					[]string{"resource.network"},
					DecisionDestroy,
				),
			},
			expected: []string{"resource.app", "resource.network"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var applied []string
			err := runApplyScheduleV2(
				context.Background(),
				test.steps,
				1,
				func(_ context.Context, step PlanStepV2) error {
					applied = append(applied, step.Address)
					return nil
				},
			)
			require.NoError(t, err)
			require.Equal(t, test.expected, applied)
		})
	}
}

func TestRunApplyScheduleV2RunsReadyStepsConcurrently(t *testing.T) {
	steps := []PlanStepV2{
		applyScheduleV2ResourceStep(t, "resource.a", []string{}, DecisionCreate),
		applyScheduleV2ResourceStep(t, "resource.b", []string{}, DecisionCreate),
		applyScheduleV2ResourceStep(
			t,
			"resource.c",
			[]string{"resource.a", "resource.b"},
			DecisionCreate,
		),
	}
	started := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseReady := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseReady)
	var completed atomic.Int32
	done := make(chan error, 1)

	go func() {
		done <- runApplyScheduleV2(
			context.Background(),
			steps,
			2,
			func(_ context.Context, step PlanStepV2) error {
				if step.Address == "resource.c" {
					if completed.Load() != 2 {
						return errors.New("dependent started before prerequisites completed")
					}
					return nil
				}
				started <- step.Address
				<-release
				completed.Add(1)
				return nil
			},
		)
	}()

	first := receiveApplyScheduleV2Start(t, started)
	second := receiveApplyScheduleV2Start(t, started)
	require.ElementsMatch(t, []string{"resource.a", "resource.b"}, []string{first, second})
	releaseReady()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		require.Fail(t, "version 2 apply scheduler did not finish")
	}
}

func TestRunApplyScheduleV2StopsDispatchingAfterFailure(t *testing.T) {
	expectedErr := errors.New("provider failed")
	steps := []PlanStepV2{
		applyScheduleV2ResourceStep(t, "resource.a", []string{}, DecisionCreate),
		applyScheduleV2ResourceStep(t, "resource.b", []string{}, DecisionCreate),
	}
	var applied []string

	err := runApplyScheduleV2(
		context.Background(),
		steps,
		1,
		func(_ context.Context, step PlanStepV2) error {
			applied = append(applied, step.Address)
			return expectedErr
		},
	)

	require.ErrorIs(t, err, expectedErr)
	require.ErrorContains(t, err, "resource.a")
	require.Equal(t, []string{"resource.a"}, applied)
}

func TestRunApplyScheduleV2RejectsInvalidStepsBeforeApply(t *testing.T) {
	valid := applyScheduleV2ResourceStep(t, "resource.a", []string{}, DecisionCreate)
	invalid := applyScheduleV2ResourceStep(t, "resource.b", []string{}, DecisionCreate)
	invalid.DependsOn = nil
	tests := []struct {
		name    string
		steps   []PlanStepV2
		message string
	}{
		{
			name:    "invalid step",
			steps:   []PlanStepV2{valid, invalid},
			message: "step 1",
		},
		{
			name:    "duplicate address",
			steps:   []PlanStepV2{valid, valid},
			message: "duplicate step address",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			err := runApplyScheduleV2(
				context.Background(),
				test.steps,
				1,
				func(context.Context, PlanStepV2) error {
					calls++
					return nil
				},
			)
			require.ErrorContains(t, err, test.message)
			require.Zero(t, calls)
		})
	}
}

func TestRunApplyScheduleV2RejectsDependencyCycle(t *testing.T) {
	steps := []PlanStepV2{
		applyScheduleV2ResourceStep(
			t,
			"resource.independent",
			[]string{},
			DecisionCreate,
		),
		applyScheduleV2ResourceStep(
			t,
			"resource.a",
			[]string{"resource.b"},
			DecisionCreate,
		),
		applyScheduleV2ResourceStep(
			t,
			"resource.b",
			[]string{"resource.a"},
			DecisionCreate,
		),
	}
	calls := 0

	err := runApplyScheduleV2(
		context.Background(),
		steps,
		2,
		func(context.Context, PlanStepV2) error {
			calls++
			return nil
		},
	)

	require.ErrorContains(t, err, "dependency cycle")
	require.Zero(t, calls)
}

func applyScheduleV2ResourceStep(
	t *testing.T,
	address string,
	dependencies []string,
	decision Decision,
) PlanStepV2 {
	t.Helper()
	operation := validResourcePlanOperation(t, decision)
	step := PlanStepV2{
		Address:   address,
		Kind:      NodeResource,
		DependsOn: dependencies,
		Operation: StepOperation{
			Kind:     StepResource,
			Resource: &operation,
		},
	}
	require.NoError(t, step.Validate())
	return step
}

func receiveApplyScheduleV2Start(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case address := <-started:
		return address
	case <-time.After(2 * time.Second):
		require.Fail(t, "ready version 2 steps did not start concurrently")
		return ""
	}
}
