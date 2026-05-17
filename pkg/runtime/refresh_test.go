package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

func TestRefreshUpdatesLeafOutputs(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	mods := resourceModules(&c)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	})

	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		for k, v := range m {
			out[k] = v
		}
		out["size"] = int64(99)
		return out, nil
	}

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
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
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	mods := resourceModules(&c)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	})

	c.readFn = func(any) (any, error) { return nil, ErrNotFound }

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
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
actions: {
  core: { echo: { hi: { echo: 'hello' } } }
}
`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), testModules()), Modules: testModules(), Store: store, Stack: stack,
	})

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), testModules()), Modules: testModules(), Store: store, Stack: stack,
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
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	mods := resourceModules(&c)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	})

	held, err := store.Lock(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Unlock() })

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = exec.Refresh(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRefreshUpdatesCompositeInternalLeaf(t *testing.T) {
	compositeBody := parseStack(t, `
inputs: {
  name: { type: string }
}

resources: {
  core: {
    thing: { inside: { name: var.name, size: 1 } }
  }
}
`)
	var c resourceCounters
	mods := resourceModules(&c)
	mods["w"] = &Module{
		Name: "w",
		Composites: map[string]*CompositeType{
			"box": {Name: "box", Body: compositeBody, Modules: mods},
		},
	}
	src := `
resources: {
  w: { box: { x: { name: 'alpha' } } }
}
`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	})

	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		for k, v := range m {
			out[k] = v
		}
		out["size"] = int64(42)
		return out, nil
	}

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	}
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.Refreshed)
	require.Equal(t, 0, res.Dropped)

	snap, err := store.Current()
	require.NoError(t, err)
	leafAddr := "resource.w.box.x/core.thing.inside"
	var leaf *state.Entry
	for _, e := range snap.Entries {
		if e.Address == leafAddr {
			leaf = e
		}
	}
	require.NotNil(t, leaf, "composite internal leaf still in snapshot after refresh")
	require.EqualValues(t, 42, leaf.Outputs["size"])
}

func TestRefreshNoPriorState(t *testing.T) {
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, `description: 'x'`), nil),
		Modules: map[string]*Module{},
		Store:   store, Stack: stack,
	}
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Refreshed)
	require.Equal(t, 0, res.Dropped)
	require.Empty(t, res.WrittenRev)
}
