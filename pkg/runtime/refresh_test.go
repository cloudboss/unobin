package runtime

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

func TestRefreshUpdatesLeafOutputs(t *testing.T) {
	src := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		maps.Copy(out, m)
		out["size"] = int64(99)
		return out, nil
	}

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	}
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.Refreshed)
	require.Equal(t, 0, res.Dropped)
	require.NotEmpty(t, res.WrittenRev)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 1)
	require.Equal(t, "resource.core.thing.one", snap.Entries[0].Address)
	require.EqualValues(t, 99, snap.Entries[0].Outputs["size"])
}

func TestRefreshDropsResourceThatIsGone(t *testing.T) {
	src := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	c.readFn = func(any) (any, error) { return nil, ErrNotFound }

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	}
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Refreshed)
	require.Equal(t, 1, res.Dropped)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Empty(t, snap.Entries)
}

func TestRefreshCarriesActionEntriesForward(t *testing.T) {
	src := `
actions: { core.echo.hi: { echo: 'hello' } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG:       BuildDAG(parseStack(t, src), testModules()),
		Libraries: testModules(),
		Store:     store,
		Factory:   stack,
	})

	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), testModules()),
		Libraries: testModules(),
		Store:     store,
		Factory:   stack,
	}
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Refreshed)
	require.Equal(t, 0, res.Dropped)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 1)
	require.Equal(t, state.EntryAction, snap.Entries[0].Type)
}

func TestRefreshWaitsForLock(t *testing.T) {
	src := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	held, err := store.Lock(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Unlock() })

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = exec.Refresh(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRefreshUpdatesCompositeInternalLeaf(t *testing.T) {
	compositeBody := parseStack(t, `
inputs: { name: { type: string } }

resources: { core.thing.inside: { name: var.name, size: 1 } }
`)
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": {Name: "box", Body: compositeBody, Libraries: libs},
		},
	}
	src := `
resources: { w.box.x: { name: 'alpha' } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		maps.Copy(out, m)
		out["size"] = int64(42)
		return out, nil
	}

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	}
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.Refreshed)
	require.Equal(t, 0, res.Dropped)

	snap, err := store.Current()
	require.NoError(t, err)
	leafAddr := "resource.w.box.x/resource.core.thing.inside"
	var leaf *state.Entry
	for _, e := range snap.Entries {
		if e.Address == leafAddr {
			leaf = e
		}
	}
	require.NotNil(t, leaf, "composite internal leaf still in snapshot after refresh")
	require.EqualValues(t, 42, leaf.Outputs["size"])
}

func TestRefreshReadsLeavesInParallel(t *testing.T) {
	const n = 6
	var src strings.Builder
	src.WriteString("resources: {\n")
	for i := range n {
		src.WriteString(fmt.Sprintf("  core.thing.r%d: { name: 'r%d', size: %d }\n", i, i, i))
	}
	src.WriteString("}\n")

	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src.String()), libs), Libraries: libs, Store: store, Factory: stack,
	})

	const delay = 150 * time.Millisecond
	c.readFn = func(prior any) (any, error) {
		time.Sleep(delay)
		return prior, nil
	}

	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src.String()), libs),
		Libraries:   libs,
		Store:       store,
		Factory:     stack,
		Parallelism: n,
	}
	start := time.Now()
	res, err := exec.Refresh(context.Background())
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Equal(t, n, res.Refreshed)
	require.Less(t, elapsed, time.Duration(n-1)*delay,
		"parallel refresh took %s; expected well under %s for serial",
		elapsed, time.Duration(n-1)*delay)
}

func TestRefreshNoPriorState(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, `description: 'x'`), nil),
		Libraries: map[string]*Library{},
		Store:     store, Factory: stack,
	}
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Refreshed)
	require.Equal(t, 0, res.Dropped)
	require.Empty(t, res.WrittenRev)
}
