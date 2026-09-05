package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type drainTrackerResource struct {
	Name  string
	Delay int64 `ub:"delay-ms"`
	runs  *atomic.Int64
}

type drainTrackerResourceOutput struct{ Name string }

func drainTrackerResourceDefinition() ResourceDefinition[
	drainTrackerResource,
	*drainTrackerResourceOutput,
	any,
] {
	return ResourceDefinition[
		drainTrackerResource,
		*drainTrackerResourceOutput,
		any,
	]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[drainTrackerResource, *drainTrackerResourceOutput]{
			Version: 1,
			Scope:   IdentityConfiguration,
		},
	}
}

func (r *drainTrackerResource) Create(
	ctx context.Context,
	_ any,
) (*drainTrackerResourceOutput, error) {
	r.runs.Add(1)
	select {
	case <-time.After(time.Duration(r.Delay) * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &drainTrackerResourceOutput{Name: r.Name}, nil
}

func (r *drainTrackerResource) Read(
	_ context.Context,
	_ any,
	_ *drainTrackerResourceOutput,
) (*drainTrackerResourceOutput, error) {
	return nil, ErrNotFound
}
func (r *drainTrackerResource) Update(
	_ context.Context, _ any, _ Prior[drainTrackerResource, *drainTrackerResourceOutput],
) (*drainTrackerResourceOutput, error) {
	return &drainTrackerResourceOutput{Name: r.Name}, nil
}
func (r *drainTrackerResource) Delete(
	_ context.Context,
	_ any,
	_ *drainTrackerResourceOutput,
) error {
	return nil
}

func drainTrackerRegistration(runs *atomic.Int64) ResourceRegistration {
	return MakeResourceWith[drainTrackerResource, *drainTrackerResourceOutput, any](
		drainTrackerResourceDefinition(),

		func() *drainTrackerResource { return &drainTrackerResource{runs: runs} },
	)
}

func TestApplyScheduleDrainStopsDispatchAndKeepsInflight(t *testing.T) {
	var runs atomic.Int64
	libs := map[string]*Library{
		"slow": {
			Name: "slow",
			Resources: map[string]ResourceRegistration{
				"r": drainTrackerRegistration(&runs),
			},
		},
	}
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-drain", "inflight"), libs)

	drain := make(chan struct{})
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  2,
		Drain:        drain,
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(drain)
	}()
	_, err := planAndApply(exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInterrupted),
		"want ErrInterrupted, got %v", err)
	assert.Equal(t, int64(2), runs.Load(),
		"only the two in-flight Creates should complete after drain")
}

func TestApplyScheduleDrainBeforeDispatchSkipsEverything(t *testing.T) {
	var runs atomic.Int64
	libs := map[string]*Library{
		"slow": {
			Name: "slow",
			Resources: map[string]ResourceRegistration{
				"r": drainTrackerRegistration(&runs),
			},
		},
	}
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-drain", "before-dispatch"), libs)
	drain := make(chan struct{})
	close(drain)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  2,
		Drain:        drain,
	}
	_, err := planAndApply(exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInterrupted),
		"want ErrInterrupted, got %v", err)
	assert.Equal(t, int64(0), runs.Load(),
		"no Create should run when drain fires before dispatch")
}
