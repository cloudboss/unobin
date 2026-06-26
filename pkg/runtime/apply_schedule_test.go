package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
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
				"r":    MakeResource[slowResource, any, any](),
				"fail": MakeResource[slowFailResource, any, any](),
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
	libs := slowLibraries()
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-schedule", "parallel-leaves"), libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  n,
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
	libs := slowLibraries()
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-schedule", "serial-leaves"), libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  1,
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
				"r": MakeResourceWith[countingSlowResource, any, any](
					func() *countingSlowResource { return &countingSlowResource{runs: &runs} },
				),
				"fail": MakeResource[slowFailResource, any, any](),
			},
		},
	}
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-schedule", "failure-drains-inflight"), libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  4,
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
				"r": MakeResourceWith[countingSlowResource, any, any](
					func() *countingSlowResource { return &countingSlowResource{runs: &runs} },
				),
				"fail": MakeResource[slowFailResource, any, any](),
			},
		},
	}
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-schedule", "skips-dependents"), libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  4,
	}
	_, err := planAndApply(exec)
	require.Error(t, err)
	assert.Equal(t, int64(0), runs.Load(),
		"downstream must not run when its upstream failed")
}

func TestApplySchedulerRunsReadyStepsInPlanOrder(t *testing.T) {
	libs := slowLibraries()
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-schedule", "ready-plan-order"), libs)
	events := make(chan ApplyEvent, 16)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  1,
		Events:       events,
	}
	_, err := planAndApply(exec)
	close(events)
	require.NoError(t, err)

	got := applyEventAddresses(readApplyEvents(events), StageStart)
	assert.Equal(t, []string{"resource.alpha", "resource.beta", "resource.gamma"}, got)
}

func TestApplySchedulerHonorsLocksWithReadyQueue(t *testing.T) {
	var track concurrencyTracker
	libs := slowActionModules(&track)
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-schedule", "ready-locks"), libs)
	events := make(chan ApplyEvent, 16)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  3,
		Events:       events,
	}
	_, err := planAndApply(exec)
	close(events)
	require.NoError(t, err)

	collected := readApplyEvents(events)
	lockedOneStart := requireApplyEvent(t, collected, "action.locked-one", StageStart)
	lockedOneDone := requireApplyEvent(t, collected, "action.locked-one", StageDone)
	lockedTwoStart := requireApplyEvent(t, collected, "action.locked-two", StageStart)
	freeStart := requireApplyEvent(t, collected, "action.free", StageStart)

	assert.True(t, lockedOneStart.Time.Before(lockedOneDone.Time))
	assert.True(t, freeStart.Time.Before(lockedOneDone.Time),
		"unrelated step should start while the first locked step is active")
	assert.False(t, lockedTwoStart.Time.Before(lockedOneDone.Time),
		"second locked step should wait for the first locked step")
	assert.Equal(t, int64(2), track.peak.Load())
}

func TestApplySchedulerPromotesDependentsOnce(t *testing.T) {
	libs := slowLibraries()
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-schedule", "diamond-dependents"), libs)
	events := make(chan ApplyEvent, 16)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  2,
		Events:       events,
	}
	_, err := planAndApply(exec)
	close(events)
	require.NoError(t, err)

	collected := readApplyEvents(events)
	joinStarts := countApplyEvents(collected, "resource.join", StageStart)
	joinStart := requireApplyEvent(t, collected, "resource.join", StageStart)
	leftDone := requireApplyEvent(t, collected, "resource.left", StageDone)
	rightDone := requireApplyEvent(t, collected, "resource.right", StageDone)

	assert.Equal(t, 1, joinStarts)
	assert.False(t, joinStart.Time.Before(leftDone.Time))
	assert.False(t, joinStart.Time.Before(rightDone.Time))
}

func TestApplySchedulerDrainKeepsCompletedState(t *testing.T) {
	libs := slowLibraries()
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-schedule", "drain-completed-state"), libs)
	store := newStateStore(t)
	drain := make(chan struct{})
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        store,
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  2,
		Drain:        drain,
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(drain)
	}()
	_, err := planAndApply(exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInterrupted), "want ErrInterrupted, got %v", err)

	snap, err := store.Current()
	require.NoError(t, err)
	got := make([]string, 0, len(snap.Entries))
	for _, ent := range snap.Entries {
		got = append(got, ent.Address)
	}
	assert.ElementsMatch(t, []string{"resource.n0", "resource.n1"}, got)
}

func BenchmarkApplySchedulerLargeIndependentPlan(b *testing.B) {
	tests := []struct {
		name  string
		graph string
	}{
		{name: "independent", graph: "independent"},
		{name: "chain", graph: "chain"},
		{name: "fan", graph: "fan"},
	}
	for _, tt := range tests {
		pf, dag := schedulerBenchmarkPlan(1000, tt.graph)
		b.Run(tt.name, func(b *testing.B) {
			benchmarkApplyScheduler(b, pf, dag)
		})
	}
}

func benchmarkApplyScheduler(b *testing.B, pf *PlanFile, dag *DAG) {
	b.Helper()
	b.ReportAllocs()
	ctx := context.Background()
	exec := &Executor{DAG: dag, Parallelism: 32}
	for b.Loop() {
		err := exec.runApplySchedule(ctx, &runState{}, pf)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func schedulerBenchmarkPlan(n int, graph string) (*PlanFile, *DAG) {
	steps := make([]PlanStep, n)
	nodes := make(map[string]*Node, n)
	edges := make(map[string][]string, n)
	for i := range n {
		addr := schedulerBenchmarkAddress(i)
		steps[i] = PlanStep{Address: addr, Kind: NodeOutput, Decision: DecisionEval}
		nodes[addr] = &Node{Address: addr, Kind: NodeOutput}
		edges[addr] = nil
	}
	switch graph {
	case "chain":
		for i := range n {
			if i == 0 {
				continue
			}
			edges[schedulerBenchmarkAddress(i)] = []string{schedulerBenchmarkAddress(i - 1)}
		}
	case "fan":
		root := schedulerBenchmarkAddress(0)
		join := schedulerBenchmarkAddress(n - 1)
		edges[join] = make([]string, 0, n-2)
		for i := range n {
			if i == 0 || i == n-1 {
				continue
			}
			mid := schedulerBenchmarkAddress(i)
			edges[mid] = []string{root}
			edges[join] = append(edges[join], mid)
		}
	}
	return &PlanFile{Steps: steps}, &DAG{Nodes: nodes, Edges: edges}
}

func schedulerBenchmarkAddress(i int) string {
	return fmt.Sprintf("output.n%04d", i)
}

func readApplyEvents(events <-chan ApplyEvent) []ApplyEvent {
	var out []ApplyEvent
	for ev := range events {
		out = append(out, ev)
	}
	return out
}

func applyEventAddresses(events []ApplyEvent, stage ApplyStage) []string {
	var out []string
	for _, ev := range events {
		if ev.Stage == stage {
			out = append(out, ev.Address)
		}
	}
	return out
}

func countApplyEvents(events []ApplyEvent, address string, stage ApplyStage) int {
	count := 0
	for _, ev := range events {
		if ev.Address == address && ev.Stage == stage {
			count++
		}
	}
	return count
}

func requireApplyEvent(
	t testing.TB,
	events []ApplyEvent,
	address string,
	stage ApplyStage,
) ApplyEvent {
	t.Helper()
	for _, ev := range events {
		if ev.Address == address && ev.Stage == stage {
			return ev
		}
	}
	require.Failf(t, "missing apply event", "address %s stage %s", address, stage)
	return ApplyEvent{}
}
