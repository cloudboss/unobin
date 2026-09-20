package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

type stableDriftResource struct {
	Name string
	Size int64

	capture *stableDriftCapture
}

type stableDriftOutput struct {
	ID         string
	Generation int64
}

type stableDriftCapture struct {
	mu       sync.Mutex
	observed *stableDriftOutput
	creates  int64
	deletes  int64
}

func (c *stableDriftCapture) setObserved(output *stableDriftOutput) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.observed = output
}

func (c *stableDriftCapture) readObserved() *stableDriftOutput {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.observed == nil {
		return nil
	}
	copy := *c.observed
	return &copy
}

func (r *stableDriftResource) Create(
	_ context.Context,
	_ any,
) (*stableDriftOutput, error) {
	atomic.AddInt64(&r.capture.creates, 1)
	return &stableDriftOutput{ID: "object-1", Generation: 1}, nil
}

func (r *stableDriftResource) Read(
	_ context.Context,
	_ any,
	prior Prior[stableDriftResource, *stableDriftOutput, any],
) (*stableDriftOutput, error) {
	if observed := r.capture.readObserved(); observed != nil {
		return observed, nil
	}
	if prior.Outputs == nil {
		return nil, ErrNotFound
	}
	return prior.Outputs, nil
}

func (r *stableDriftResource) Update(
	_ context.Context,
	_ any,
	prior Prior[stableDriftResource, *stableDriftOutput, any],
) (*stableDriftOutput, error) {
	return prior.Outputs, nil
}

func (r *stableDriftResource) Delete(
	_ context.Context,
	_ any,
	_ Prior[stableDriftResource, *stableDriftOutput, any],
) error {
	atomic.AddInt64(&r.capture.deletes, 1)
	return nil
}

func stableDriftDefinition() ResourceDefinition[
	stableDriftResource,
	*stableDriftOutput,
	any,
] {
	generation := OutputField(func(output *stableDriftOutput) *int64 {
		return &output.Generation
	})
	return ResourceDefinition[stableDriftResource, *stableDriftOutput, any]{
		SchemaVersion: 1,
		Replace: Replacement[stableDriftResource, *stableDriftOutput, any]{
			Drift: []DriftRule[*stableDriftOutput]{
				ReplaceOnDrift(generation, func(recorded, observed int64) bool {
					return recorded != observed
				}),
			},
		},
		StableID: func(_ stableDriftResource, output *stableDriftOutput) (string, error) {
			return output.ID, nil
		},
	}
}

func stableDriftModules(capture *stableDriftCapture) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"inc": MakeResourceWith[stableDriftResource, *stableDriftOutput, any](
					stableDriftDefinition(),
					func() *stableDriftResource {
						return &stableDriftResource{capture: capture}
					},
				),
			},
		},
	}
}

func stableDriftExecutor(
	t *testing.T,
	capture *stableDriftCapture,
	store state.Backend,
) *Executor {
	t.Helper()
	return applyPlanTestExecutor(
		t,
		applyPlanFixture(t, "replacement-new"),
		stableDriftModules(capture),
		store,
		state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	)
}

func TestSelectedDriftReplacesWithStableID(t *testing.T) {
	capture := &stableDriftCapture{}
	store := newStateStore(t)
	applyOnce(t, stableDriftExecutor(t, capture, store))
	capture.setObserved(&stableDriftOutput{ID: "object-1", Generation: 2})

	exec := stableDriftExecutor(t, capture, store)
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	step := findStep(t, plan, "resource.one")
	require.Equal(t, DecisionReplace, step.Decision)
	require.Equal(t, []string{"generation"}, step.ReplacementReasons)
	require.NotNil(t, step.ExpectedStableID)
	require.Equal(t, "object-1", *step.ExpectedStableID)

	capture.setObserved(&stableDriftOutput{ID: "object-2", Generation: 2})
	_, err = planAndApplyExisting(exec, plan)
	require.ErrorContains(t, err, `stable ID changed from "object-1" to "object-2"`)
	require.Equal(t, int64(0), atomic.LoadInt64(&capture.deletes))
	require.Equal(t, int64(1), atomic.LoadInt64(&capture.creates))
}

func TestChangedStableIDStopsPlanning(t *testing.T) {
	capture := &stableDriftCapture{}
	store := newStateStore(t)
	applyOnce(t, stableDriftExecutor(t, capture, store))
	capture.setObserved(&stableDriftOutput{ID: "object-2", Generation: 2})

	_, err := stableDriftExecutor(t, capture, store).Plan(context.Background())
	require.ErrorContains(t, err, `stable ID changed from "object-1" to "object-2"`)
	require.Equal(t, int64(0), atomic.LoadInt64(&capture.deletes))
}
