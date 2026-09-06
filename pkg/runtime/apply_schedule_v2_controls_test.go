package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestApplyScheduleV2LocksAllowIndependentWork(t *testing.T) {
	steps := []PlanStepV2{
		applyScheduleV2ResourceStep(t, "resource.a", []string{}, DecisionCreate),
		applyScheduleV2ResourceStep(t, "resource.b", []string{}, DecisionCreate),
		applyScheduleV2ResourceStep(t, "resource.c", []string{}, DecisionCreate),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	released := make(chan struct{})
	var finished atomic.Bool
	events := make(chan ApplyEvent, 6)
	err := runApplyScheduleV2WithOptions(ctx, steps, 2, applyScheduleV2Options{
		Nodes: map[string]*Node{
			"resource.a": {LockName: "serial"}, "resource.b": {LockName: "serial"},
		},
		Events: events,
	}, func(ctx context.Context, step PlanStepV2) error {
		switch step.Address {
		case "resource.a":
			select {
			case <-released:
				finished.Store(true)
			case <-ctx.Done():
				return ctx.Err()
			}
		case "resource.b":
			if !finished.Load() {
				return errors.New("named lock allowed overlapping operations")
			}
		case "resource.c":
			close(released)
		}
		return nil
	})
	require.NoError(t, err)
	close(events)
	stages := map[string][]ApplyStage{}
	for event := range events {
		require.False(t, event.Time.IsZero())
		stages[event.Address] = append(stages[event.Address], event.Stage)
	}
	for _, step := range steps {
		require.Equal(t, []ApplyStage{StageStart, StageDone}, stages[step.Address])
	}
}

func TestApplyScheduleV2ReportsTimeoutAndSkippedWork(t *testing.T) {
	steps := []PlanStepV2{
		applyScheduleV2ResourceStep(t, "resource.a", []string{}, DecisionCreate),
		applyScheduleV2ResourceStep(t, "resource.b", []string{"resource.a"}, DecisionCreate),
	}
	events := make(chan ApplyEvent, 4)
	calls := 0
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := runApplyScheduleV2WithOptions(ctx, steps, 1, applyScheduleV2Options{
		Nodes: map[string]*Node{
			"resource.a": {Alias: "cloud", Timeout: 5 * time.Millisecond},
		},
		Events: events,
	}, func(ctx context.Context, _ PlanStepV2) error {
		calls++
		<-ctx.Done()
		return ctx.Err()
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 1, calls)
	var failure *ApplyError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "resource.a", failure.Address)
	require.Equal(t, "cloud", failure.Alias)
	require.Equal(t, steps[0].Operation.Resource.Desired.Binding.LibraryPath, failure.LibraryPath)
	require.Equal(t, 1, failure.SkippedCount)
	require.Zero(t, failure.SucceededCount)
	require.Less(t, failure.Elapsed, time.Second)
	require.Equal(t, StageStart, (<-events).Stage)
	require.Equal(t, StageFail, (<-events).Stage)
}

func TestApplyScheduleV2DrainsWithoutCancelingWork(t *testing.T) {
	for _, alreadyClosed := range []bool{false, true} {
		t.Run(map[bool]string{false: "in flight", true: "before dispatch"}[alreadyClosed],
			func(t *testing.T) {
				drain := make(chan struct{})
				if alreadyClosed {
					close(drain)
				}
				steps := []PlanStepV2{
					applyScheduleV2ResourceStep(t, "resource.a", []string{}, DecisionCreate),
					applyScheduleV2ResourceStep(t, "resource.b", []string{}, DecisionCreate),
				}
				completed := 0
				err := runApplyScheduleV2WithOptions(context.Background(), steps, 1,
					applyScheduleV2Options{Drain: drain},
					func(ctx context.Context, _ PlanStepV2) error {
						close(drain)
						if err := ctx.Err(); err != nil {
							return err
						}
						completed++
						return nil
					},
				)
				require.ErrorIs(t, err, ErrInterrupted)
				require.Equal(t, map[bool]int{false: 1, true: 0}[alreadyClosed], completed)
			},
		)
	}
}
