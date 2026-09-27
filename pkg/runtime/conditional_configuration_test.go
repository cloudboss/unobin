package runtime

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type conditionalReadRegistration struct {
	ResourceRegistration
	missing bool
}

func (r *conditionalReadRegistration) Read(
	ctx context.Context, receiver, config any, prior resourcePrior,
) (any, error) {
	if r.missing {
		return nil, ErrNotFound
	}
	return r.ResourceRegistration.Read(ctx, receiver, config, prior)
}

func TestDeferredConfigurationResolvesObservationBeforeMutation(t *testing.T) {
	tests := []struct {
		name     string
		missing  bool
		observed *stableDriftOutput
		want     Decision
		wantErr  string
	}{
		{name: "unchanged", want: DecisionNoOp},
		{name: "missing", missing: true, want: DecisionCreate},
		{
			name: "replacement drift", want: DecisionReplace,
			observed: &stableDriftOutput{ID: "object-1", Generation: 2},
		},
		{
			name: "changed identity", wantErr: "stable ID changed",
			observed: &stableDriftOutput{ID: "object-2", Generation: 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := &stableDriftCapture{}
			registration := &conditionalReadRegistration{
				ResourceRegistration: MakeResourceWith[stableDriftResource, *stableDriftOutput, any](
					stableDriftDefinition(),
					func() *stableDriftResource { return &stableDriftResource{capture: capture} },
				),
			}
			libs := configuredLibraries()
			libs["fix"].Resources["config-echo"] = registration
			source := ubtest.ReadValidFixture(t, "testdata/ub/apply-configuration", "input-config")
			exec := configurationTestExecutor(t, source, libs)
			exec.Store = newStateStore(t)
			exec.Factory = state.FactoryInfo{Name: "deferred", Version: "v0", ContentRevision: "c0"}
			exec.Inputs = map[string]any{"url": "one"}
			applyOnce(t, exec)
			fresh := configurationTestExecutor(t, source, libs)
			fresh.Store, fresh.Factory = exec.Store, exec.Factory
			exec = fresh
			exec.Inputs = map[string]any{"url": "two"}
			plan, err := exec.Plan(context.Background())
			require.NoError(t, err)
			step := findStep(t, plan, "resource.app")
			require.Equal(t, []string{"generation"}, step.PendingReplacementReasons)
			require.Empty(t, step.ReplacementReasons)
			registration.missing = tt.missing
			capture.setObserved(tt.observed)
			events := make(chan ApplyEvent, 32)
			exec.Events = events
			_, err = planAndApplyExisting(exec, plan)
			close(events)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Zero(t, atomic.LoadInt64(&capture.deletes))
				require.Equal(t, int64(1), atomic.LoadInt64(&capture.creates))
				return
			}
			require.NoError(t, err)
			got := requireApplyEvent(t, readApplyEvents(events), "resource.app", StageDone)
			require.Equal(t, tt.want, got.Decision)
			creates, deletes := int64(1), int64(0)
			if tt.want == DecisionCreate || tt.want == DecisionReplace {
				creates++
			}
			if tt.want == DecisionReplace {
				deletes++
			}
			require.Equal(t, creates, atomic.LoadInt64(&capture.creates))
			require.Equal(t, deletes, atomic.LoadInt64(&capture.deletes))
		})
	}
}
