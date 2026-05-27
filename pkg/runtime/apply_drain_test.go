package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type drainTrackerResource struct {
	Name  string
	Delay int64 `ub:"delay-ms"`
	runs  *atomic.Int64
}

func (r *drainTrackerResource) SchemaVersion() int { return 1 }

func (r *drainTrackerResource) Create(ctx context.Context, _ any) (any, error) {
	r.runs.Add(1)
	select {
	case <-time.After(time.Duration(r.Delay) * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return map[string]any{"name": r.Name}, nil
}

func (r *drainTrackerResource) Read(_ context.Context, _, _ any) (any, error) {
	return nil, ErrNotFound
}
func (r *drainTrackerResource) Update(_ context.Context, _, _ any) (any, error) {
	return map[string]any{"name": r.Name}, nil
}
func (r *drainTrackerResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *drainTrackerResource) ReplaceFields() []string                  { return nil }

func drainTrackerRegistration(runs *atomic.Int64) ResourceRegistration {
	return MakeResourceWith[drainTrackerResource, any](
		func() *drainTrackerResource { return &drainTrackerResource{runs: runs} },
	)
}

func TestApplyScheduleDrainStopsDispatchAndKeepsInflight(t *testing.T) {
	var runs atomic.Int64
	mods := map[string]*Module{
		"slow": {
			Name: "slow",
			Resources: map[string]ResourceRegistration{
				"r": drainTrackerRegistration(&runs),
			},
		},
	}
	var src strings.Builder
	src.WriteString("resources: {\n  slow: {\n    r: {\n")
	for i := range 6 {
		src.WriteString(fmt.Sprintf("      n%d: { name: 'n%d', delay-ms: 200 }\n", i, i))
	}
	src.WriteString("    }\n  }\n}\n")

	drain := make(chan struct{})
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src.String()), mods),
		Modules:     mods,
		Store:       newStateStore(t),
		Stack:       state.StackInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism: 2,
		Drain:       drain,
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
	mods := map[string]*Module{
		"slow": {
			Name: "slow",
			Resources: map[string]ResourceRegistration{
				"r": drainTrackerRegistration(&runs),
			},
		},
	}
	src := `
resources: {
  slow: {
    r: {
      n0: { name: 'n0', delay-ms: 100 }
    }
  }
}
`
	drain := make(chan struct{})
	close(drain)
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src), mods),
		Modules:     mods,
		Store:       newStateStore(t),
		Stack:       state.StackInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism: 2,
		Drain:       drain,
	}
	_, err := planAndApply(exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInterrupted),
		"want ErrInterrupted, got %v", err)
	assert.Equal(t, int64(0), runs.Load(),
		"no Create should run when drain fires before dispatch")
}
