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

type slowResource struct {
	Name  string
	Delay int64 `ub:"delay-ms"`
}

func (r *slowResource) SchemaVersion() int { return 1 }

func (r *slowResource) Create(ctx context.Context, _ any) (any, error) {
	select {
	case <-time.After(time.Duration(r.Delay) * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return map[string]any{"name": r.Name}, nil
}

func (r *slowResource) Read(_ context.Context, _, _ any) (any, error) {
	return nil, ErrNotFound
}
func (r *slowResource) Update(
	_ context.Context, _ any, _ Prior[slowResource, any],
) (any, error) {
	return map[string]any{"name": r.Name}, nil
}
func (r *slowResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *slowResource) ReplaceFields() []string                  { return nil }

type slowFailResource struct {
	Name  string
	Delay int64 `ub:"delay-ms"`
}

func (r *slowFailResource) SchemaVersion() int { return 1 }

func (r *slowFailResource) Create(ctx context.Context, _ any) (any, error) {
	select {
	case <-time.After(time.Duration(r.Delay) * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return nil, fmt.Errorf("slow-fail %s", r.Name)
}

func (r *slowFailResource) Read(_ context.Context, _, _ any) (any, error) {
	return nil, ErrNotFound
}
func (r *slowFailResource) Update(
	_ context.Context, _ any, _ Prior[slowFailResource, any],
) (any, error) {
	return nil, errors.New("unreachable")
}
func (r *slowFailResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *slowFailResource) ReplaceFields() []string                  { return nil }

func slowLibraries() map[string]*Library {
	return map[string]*Library{
		"slow": {
			Name: "slow",
			Resources: map[string]ResourceRegistration{
				"r":    MakeResource[slowResource, any](),
				"fail": MakeResource[slowFailResource, any](),
			},
		},
	}
}

func TestApplyScheduleRunsIndependentLeavesInParallel(t *testing.T) {
	const (
		n        = 8
		delay    = 200 * time.Millisecond
		serialUB = 7 * delay
	)
	var src strings.Builder
	src.WriteString("resources: {\n")
	for i := range n {
		src.WriteString(fmt.Sprintf("  slow.r.n%d: { name: 'n%d', delay-ms: %d }\n",
			i, i, delay.Milliseconds()))
	}
	src.WriteString("}\n")

	libs := slowLibraries()
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src.String()), libs),
		Libraries:   libs,
		Store:       newStateStore(t),
		Factory:     state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism: n,
	}
	start := time.Now()
	_, err := planAndApply(exec)
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.Less(t, elapsed, serialUB,
		"parallel apply took %s; expected well under %s for serial",
		elapsed, serialUB)
}

func TestApplyScheduleP1IsSerial(t *testing.T) {
	const (
		n     = 4
		delay = 100 * time.Millisecond
	)
	var src strings.Builder
	src.WriteString("resources: {\n")
	for i := range n {
		src.WriteString(fmt.Sprintf("  slow.r.n%d: { name: 'n%d', delay-ms: %d }\n",
			i, i, delay.Milliseconds()))
	}
	src.WriteString("}\n")

	libs := slowLibraries()
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src.String()), libs),
		Libraries:   libs,
		Store:       newStateStore(t),
		Factory:     state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism: 1,
	}
	start := time.Now()
	_, err := planAndApply(exec)
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, time.Duration(n-1)*delay,
		"p=1 apply took %s; expected at least %s for serial",
		elapsed, time.Duration(n-1)*delay)
}

type countingSlowResource struct {
	Name  string
	Delay int64 `ub:"delay-ms"`
	runs  *atomic.Int64
}

func (r *countingSlowResource) SchemaVersion() int { return 1 }

func (r *countingSlowResource) Create(ctx context.Context, _ any) (any, error) {
	r.runs.Add(1)
	select {
	case <-time.After(time.Duration(r.Delay) * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return map[string]any{"name": r.Name}, nil
}

func (r *countingSlowResource) Read(_ context.Context, _, _ any) (any, error) {
	return nil, ErrNotFound
}
func (r *countingSlowResource) Update(
	_ context.Context, _ any, _ Prior[countingSlowResource, any],
) (any, error) {
	return map[string]any{"name": r.Name}, nil
}
func (r *countingSlowResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *countingSlowResource) ReplaceFields() []string                  { return nil }

func TestApplyScheduleFailureStopsDispatchButDrainsInflight(t *testing.T) {
	var runs atomic.Int64
	libs := map[string]*Library{
		"slow": {
			Name: "slow",
			Resources: map[string]ResourceRegistration{
				"r": MakeResourceWith[countingSlowResource, any](
					func() *countingSlowResource { return &countingSlowResource{runs: &runs} },
				),
				"fail": MakeResource[slowFailResource, any](),
			},
		},
	}
	src := `
resources: {
  slow.fail.boom: { name: 'boom', delay-ms: 50 }
  slow.r.a:       { name: 'a', delay-ms: 300 }
  slow.r.b:       { name: 'b', delay-ms: 300 }
}
`
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src), libs),
		Libraries:   libs,
		Store:       newStateStore(t),
		Factory:     state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism: 4,
	}
	_, err := planAndApply(exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slow-fail")
	assert.Equal(t, int64(2), runs.Load(),
		"both slow siblings should have run to completion even though boom failed")
}

func TestApplyScheduleSkipsTransitiveDependentsOfFailure(t *testing.T) {
	var runs atomic.Int64
	libs := map[string]*Library{
		"slow": {
			Name: "slow",
			Resources: map[string]ResourceRegistration{
				"r": MakeResourceWith[countingSlowResource, any](
					func() *countingSlowResource { return &countingSlowResource{runs: &runs} },
				),
				"fail": MakeResource[slowFailResource, any](),
			},
		},
	}
	src := `
resources: {
  slow.fail.upstream: { name: 'upstream', delay-ms: 10 }
  slow.r.downstream:  { name: resource.slow.fail.upstream.name, delay-ms: 100 }
}
`
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src), libs),
		Libraries:   libs,
		Store:       newStateStore(t),
		Factory:     state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism: 4,
	}
	_, err := planAndApply(exec)
	require.Error(t, err)
	assert.Equal(t, int64(0), runs.Load(),
		"downstream must not run when its upstream failed")
}
