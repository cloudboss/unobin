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
	Delay int64 `ub:"delay-ms"`
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

func slowActionModules(track *concurrencyTracker) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Actions: map[string]ActionRegistration{
				"slow": MakeActionWith[slowAction, any, any](
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
  a: core.slow { @lock: 'kubectl', delay-ms: 100 }
  b: core.slow { @lock: 'kubectl', delay-ms: 100 }
  c: core.slow { @lock: 'kubectl', delay-ms: 100 }
}
`
	libs := slowActionModules(&track)
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  4,
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
  a: core.slow { @lock: 'one', delay-ms: 100 }
  b: core.slow { @lock: 'two', delay-ms: 100 }
  c: core.slow { @lock: 'three', delay-ms: 100 }
}
`
	libs := slowActionModules(&track)
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  4,
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
  a:    core.slow { @lock: 'kubectl', delay-ms: 100 }
  b:    core.slow { @lock: 'kubectl', delay-ms: 100 }
  free: core.slow { delay-ms: 100 }
}
`
	libs := slowActionModules(&track)
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  4,
	}
	_, err := planAndApply(exec)
	require.NoError(t, err)
	assert.Equal(t, int64(2), track.peak.Load(),
		"the unlocked action should run alongside one locked action")
}

func TestExtractLockName(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "action",
			src:  `actions: { x: core.slow { @lock: 'kubectl', delay-ms: 50 } }`,
			want: "kubectl",
		},
		{
			name: "resource",
			src:  `resources: { x: aws.sg-rule { @lock: 'sg', port: 80 } }`,
			want: "sg",
		},
		{
			name: "data",
			src:  `data: { x: aws.ami { @lock: 'reads', most-recent: true } }`,
			want: "reads",
		},
		{
			name: "no lock",
			src:  `resources: { x: aws.vpc { cidr: '10.0.0.0/16' } }`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := ExtractSyntaxNodes(syntaxFactoryBody(t, tt.src), nil)
			require.Len(t, nodes, 1)
			assert.Equal(t, tt.want, nodes[0].LockName)
		})
	}
}
