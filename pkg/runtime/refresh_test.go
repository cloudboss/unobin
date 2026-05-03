package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/state"
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
	_, err := (&Executor{
		DAG: BuildDAG(parseStack(t, src)), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

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
		DAG: BuildDAG(parseStack(t, src)), Modules: mods, Store: store, Stack: stack,
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
	_, err := (&Executor{
		DAG: BuildDAG(parseStack(t, src)), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

	c.readFn = func(any) (any, error) { return nil, ErrNotFound }

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src)), Modules: mods, Store: store, Stack: stack,
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
	_, err := (&Executor{
		DAG: BuildDAG(parseStack(t, src)), Modules: testModules(), Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src)), Modules: testModules(), Store: store, Stack: stack,
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
	_, err := (&Executor{
		DAG: BuildDAG(parseStack(t, src)), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

	held, err := store.Lock(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Unlock() })

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src)), Modules: mods, Store: store, Stack: stack,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = exec.Refresh(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRefreshNoPriorState(t *testing.T) {
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	exec := &Executor{
		DAG: BuildDAG(parseStack(t, `description: 'x'`)), Modules: map[string]*Module{},
		Store: store, Stack: stack,
	}
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Refreshed)
	require.Equal(t, 0, res.Dropped)
	require.Empty(t, res.WrittenRev)
}
