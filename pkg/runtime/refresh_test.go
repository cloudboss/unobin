package runtime

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/ubtest"
)

func refreshFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/refresh", name)
}

func refreshTestExecutor(
	t *testing.T,
	src string,
	libs map[string]*Library,
	store state.Backend,
	stack state.FactoryInfo,
) *Executor {
	t.Helper()
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	return &Executor{
		DAG: dag, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	}
}

func TestRefreshUpdatesLeafOutputs(t *testing.T) {
	src := refreshFixture(t, "resource-one")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, refreshTestExecutor(t, src, libs, store, stack))

	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		maps.Copy(out, m)
		out["size"] = int64(99)
		return out, nil
	}

	exec := refreshTestExecutor(t, src, libs, store, stack)
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.Refreshed)
	require.Equal(t, 0, res.Dropped)
	require.NotEmpty(t, res.WrittenRev)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 1)
	require.Equal(t, "resource.one", snap.Entries[0].Address)
	require.EqualValues(t, 99, snap.Entries[0].Outputs["size"])
}

func TestRefreshUsesShortAddressBinding(t *testing.T) {
	src := refreshFixture(t, "resource-one")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, refreshTestExecutor(t, src, libs, store, stack))

	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		maps.Copy(out, m)
		out["size"] = int64(99)
		return out, nil
	}

	exec := refreshTestExecutor(t, src, libs, store, stack)
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.Refreshed)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 1)
	require.Equal(t, "resource.one", snap.Entries[0].Address)
	require.EqualValues(t, 99, snap.Entries[0].Outputs["size"])
}

func TestDestroyUsesShortAddressBinding(t *testing.T) {
	src := refreshFixture(t, "resource-one")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, refreshTestExecutor(t, src, libs, store, stack))

	applyOnce(t, refreshTestExecutor(t, ``, libs, store, stack))
	require.EqualValues(t, 1, c.deletes)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Empty(t, snap.Entries)
}

func TestRefreshDropsResourceThatIsGone(t *testing.T) {
	src := refreshFixture(t, "resource-one")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, refreshTestExecutor(t, src, libs, store, stack))

	c.readFn = func(any) (any, error) { return nil, ErrNotFound }

	exec := refreshTestExecutor(t, src, libs, store, stack)
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Refreshed)
	require.Equal(t, 1, res.Dropped)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Empty(t, snap.Entries)
}

func TestRefreshPreservesActionEntries(t *testing.T) {
	src := refreshFixture(t, "action-hi")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, refreshTestExecutor(t, src, testModules(), store, stack))

	exec := refreshTestExecutor(t, src, testModules(), store, stack)
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
	src := refreshFixture(t, "resource-one")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, refreshTestExecutor(t, src, libs, store, stack))

	held, err := store.Lock(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Unlock() })

	exec := refreshTestExecutor(t, src, libs, store, stack)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = exec.Refresh(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRefreshUpdatesCompositeInternalLeaf(t *testing.T) {
	compositeBody := syntaxResourceComposite(t, "box", refreshFixture(t, "composite-box"))
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": compositeBody,
		},
	}
	src := refreshFixture(t, "composite-call")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, refreshTestExecutor(t, src, libs, store, stack))

	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		maps.Copy(out, m)
		out["size"] = int64(42)
		return out, nil
	}

	exec := refreshTestExecutor(t, src, libs, store, stack)
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.Refreshed)
	require.Equal(t, 0, res.Dropped)

	snap, err := store.Current()
	require.NoError(t, err)
	leafAddr := "resource.x/resource.inside"
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
	src := refreshFixture(t, "parallel-resources")

	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, refreshTestExecutor(t, src, libs, store, stack))

	const delay = 150 * time.Millisecond
	c.readFn = func(prior any) (any, error) {
		time.Sleep(delay)
		return prior, nil
	}

	exec := refreshTestExecutor(t, src, libs, store, stack)
	exec.Parallelism = n
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
	exec := refreshTestExecutor(t, `description: 'x'`, map[string]*Library{}, store, stack)
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Refreshed)
	require.Equal(t, 0, res.Dropped)
	require.Empty(t, res.WrittenRev)
}

func TestRefreshMigratesPriorEntry(t *testing.T) {
	// A v1 entry is refreshed under a v2 resource whose Migrate renames
	// the input `label` to `name` and the output `id` to `name-id`. The
	// rewritten entry must hold the migrated inputs stamped at the current
	// version, not the old inputs stamped current, which would strand a
	// later input migration.
	src := refreshFixture(t, "resource-one")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	prior := state.NewSnapshot(stack, store.Stack())
	prior.Entries = []*state.Entry{{
		Address:       "resource.one",
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Binding:       &state.Binding{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"label": "alpha", "size": float64(1)},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha", "size": float64(1)},
	}}
	rev, err := store.Write(prior)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	var c resourceCounters
	libs := inputMigratingLibs(&c)
	exec := refreshTestExecutor(t, src, libs, store, stack)
	res, err := exec.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.Refreshed)

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.one")
	require.NotNil(t, ent)
	require.Equal(t, 2, ent.SchemaVersion)
	require.NotContains(t, ent.Inputs, "label")
	require.Equal(t, "alpha", ent.Inputs["name"])
	require.NotContains(t, ent.Outputs, "id")
	require.Equal(t, "fake-alpha", ent.Outputs["name-id"])
}

func TestRefreshDoesNotInventDefaults(t *testing.T) {
	// The defaults overlay is a plan-time concern. Refresh keeps prior
	// inputs as they were read, so a field that exists only as a declared
	// default is not invented into refreshed state.
	src := refreshFixture(t, "resource-one-no-size")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, &state.Entry{
		Address:       "resource.one",
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Binding:       &state.Binding{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha"},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha"},
	})

	var c resourceCounters
	libs := defaultingLibs(&c)
	exec := refreshTestExecutor(t, src, libs, store, stack)
	_, err := exec.Refresh(context.Background())
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.one")
	require.NotNil(t, ent)
	require.NotContains(t, ent.Inputs, "size",
		"refresh keeps prior inputs as read; it does not apply defaults")
}
