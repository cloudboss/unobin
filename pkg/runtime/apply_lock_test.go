package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type concurrencyTracker struct {
	current atomic.Int64
	peak    atomic.Int64
}

func (c *concurrencyTracker) enter() {
	now := c.current.Add(1)
	for {
		peak := c.peak.Load()
		if now <= peak {
			return
		}
		if c.peak.CompareAndSwap(peak, now) {
			return
		}
	}
}

func (c *concurrencyTracker) leave() {
	c.current.Add(-1)
}

type slowAction struct {
	Delay int64 `mapstructure:"delay-ms"`
	track *concurrencyTracker
}

func (a *slowAction) Run(ctx context.Context, _ any) (any, error) {
	a.track.enter()
	defer a.track.leave()
	select {
	case <-time.After(time.Duration(a.Delay) * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return map[string]any{}, nil
}

func slowActionModules(track *concurrencyTracker) map[string]*Module {
	return map[string]*Module{
		"core": {
			Name: "core",
			Actions: map[string]ActionRegistration{
				"slow": MakeActionWith[slowAction, any](
					func() *slowAction { return &slowAction{track: track} },
				),
			},
		},
	}
}

func TestApplyScheduleLockSerializesNamedActions(t *testing.T) {
	var track concurrencyTracker
	src := `
actions: {
  core: {
    slow: {
      a: { @lock: 'kubectl', delay-ms: 100 }
      b: { @lock: 'kubectl', delay-ms: 100 }
      c: { @lock: 'kubectl', delay-ms: 100 }
    }
  }
}
`
	mods := slowActionModules(&track)
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src), mods),
		Modules:     mods,
		Store:       newStateStore(t),
		Stack:       state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
		Parallelism: 4,
	}
	_, err := planAndApply(exec)
	require.NoError(t, err)
	assert.Equal(t, int64(1), track.peak.Load(),
		"actions sharing a lock must not run concurrently")
}

func TestApplyScheduleDistinctLocksRunInParallel(t *testing.T) {
	var track concurrencyTracker
	src := `
actions: {
  core: {
    slow: {
      a: { @lock: 'one', delay-ms: 100 }
      b: { @lock: 'two', delay-ms: 100 }
      c: { @lock: 'three', delay-ms: 100 }
    }
  }
}
`
	mods := slowActionModules(&track)
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src), mods),
		Modules:     mods,
		Store:       newStateStore(t),
		Stack:       state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
		Parallelism: 4,
	}
	_, err := planAndApply(exec)
	require.NoError(t, err)
	assert.Equal(t, int64(3), track.peak.Load(),
		"distinct lock names should not serialize independent actions")
}

func TestApplyScheduleUnlockedActionRunsAlongsideLocked(t *testing.T) {
	var track concurrencyTracker
	src := `
actions: {
  core: {
    slow: {
      a: { @lock: 'kubectl', delay-ms: 100 }
      b: { @lock: 'kubectl', delay-ms: 100 }
      free: { delay-ms: 100 }
    }
  }
}
`
	mods := slowActionModules(&track)
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src), mods),
		Modules:     mods,
		Store:       newStateStore(t),
		Stack:       state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
		Parallelism: 4,
	}
	_, err := planAndApply(exec)
	require.NoError(t, err)
	assert.Equal(t, int64(2), track.peak.Load(),
		"the unlocked action should run alongside one locked action")
}

func TestExtractLockName(t *testing.T) {
	src := `
actions: {
  core: {
    slow: {
      x: { @lock: 'kubectl', delay-ms: 50 }
    }
  }
}
`
	f := parseStack(t, src)
	nodes := ExtractNodes(f, nil)
	require.Len(t, nodes, 1)
	assert.Equal(t, "kubectl", nodes[0].LockName)
}
