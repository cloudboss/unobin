package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/localstate"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

// deleteOrder records the order resources were deleted in, so a test
// can confirm destroys ran in reverse dependency order.
type deleteOrder struct {
	mu    sync.Mutex
	order []string
}

type orderResource struct {
	Name string
	Dep  string

	rec *deleteOrder
}

func (r *orderResource) Create(_ context.Context, _ any) (any, error) {
	return map[string]any{"id": "id-" + r.Name, "name": r.Name}, nil
}

func (r *orderResource) Read(_ context.Context, _ any, prior any) (any, error) {
	return prior, nil
}

func (r *orderResource) Update(
	_ context.Context, _ any, prior Prior[orderResource, any],
) (any, error) {
	return prior.Outputs, nil
}

func (r *orderResource) Delete(_ context.Context, _ any, _ any) error {
	r.rec.mu.Lock()
	r.rec.order = append(r.rec.order, r.Name)
	r.rec.mu.Unlock()
	return nil
}

func (r *orderResource) ReplaceFields() []string { return nil }

func (r *orderResource) SchemaVersion() int { return 1 }

func orderModules(rec *deleteOrder) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[orderResource, any](
					func() *orderResource { return &orderResource{rec: rec} },
				),
			},
		},
	}
}

func TestDestroyDeletesDependentsFirst(t *testing.T) {
	rec := &deleteOrder{}
	libs := orderModules(rec)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	withBoth := `
resources: {
  core: {
    thing: {
      a: { name: 'a' }
      b: { name: 'b', dep: resource.core.thing.a.id }
    }
  }
}
`
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, withBoth), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	_, err := planAndApply(exec)
	require.NoError(t, err)

	// Remove both from source so the next apply destroys them. b
	// depended on a, so b must be deleted before a.
	empty := &Executor{
		DAG:       BuildDAG(parseStack(t, `description: 'gone'`), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	_, err = planAndApply(empty)
	require.NoError(t, err)
	require.Equal(t, []string{"b", "a"}, rec.order)
}

// cfgCapture records the configuration value a cfgResource was handed
// at delete time, so a test can confirm destroy used the right one.
type cfgCapture struct {
	deleteCfg any
	deleted   bool
}

type cfgResource struct {
	Name string

	capture *cfgCapture
}

func (r *cfgResource) Create(_ context.Context, _ any) (any, error) {
	return map[string]any{"id": "id-" + r.Name}, nil
}

func (r *cfgResource) Read(_ context.Context, _ any, prior any) (any, error) {
	return prior, nil
}

func (r *cfgResource) Update(
	_ context.Context, _ any, prior Prior[cfgResource, any],
) (any, error) {
	return prior.Outputs, nil
}

func (r *cfgResource) Delete(_ context.Context, cfg any, _ any) error {
	r.capture.deleteCfg = cfg
	r.capture.deleted = true
	return nil
}

func (r *cfgResource) ReplaceFields() []string { return nil }

func (r *cfgResource) SchemaVersion() int { return 1 }

func cfgCapturingModules(capture *cfgCapture) map[string]*Library {
	return map[string]*Library{
		"aws": {
			Name:          "aws",
			Configuration: &cfg.ConfigurationType{New: func() any { return &struct{}{} }},
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[cfgResource, any](
					func() *cfgResource { return &cfgResource{capture: capture} },
				),
			},
		},
	}
}

func TestDestroyUsesRecordedConfiguration(t *testing.T) {
	capture := &cfgCapture{}
	libs := cfgCapturingModules(capture)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	configurations := map[string]map[string]any{
		"aws": {"default": "default-cfg", "east2": "east2-cfg"},
	}

	withResource := `
resources: {
  aws: {
    thing: {
      x: {
        @configuration: aws.east2
        name:           'x'
      }
    }
  }
}
`
	exec := &Executor{
		DAG:            BuildDAG(parseStack(t, withResource), libs),
		Libraries:      libs,
		Configurations: configurations,
		Store:          store,
		Factory:        stack,
	}
	_, err := planAndApply(exec)
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 1)
	require.Equal(t, "aws.east2", snap.Entries[0].Configuration)

	// Remove the resource from source so the next apply destroys it,
	// and confirm Delete ran against the east2 configuration.
	empty := &Executor{
		DAG:            BuildDAG(parseStack(t, `description: 'gone'`), libs),
		Libraries:      libs,
		Configurations: configurations,
		Store:          store,
		Factory:        stack,
	}
	_, err = planAndApply(empty)
	require.NoError(t, err)
	require.True(t, capture.deleted)
	require.Equal(t, "east2-cfg", capture.deleteCfg)

	snap, err = store.Current()
	require.NoError(t, err)
	require.Empty(t, snap.Entries)
}

var errIncrementalResource = errors.New("intentional resource failure")

type incrementalResourceCounters struct {
	creates int64
	updates int64
	deletes int64
}

type incrementalResource struct {
	Name string
	Size int64

	counters *incrementalResourceCounters
}

func (r *incrementalResource) Create(_ context.Context, _ any) (any, error) {
	if r.Name == "fail-create" {
		return nil, errIncrementalResource
	}
	atomic.AddInt64(&r.counters.creates, 1)
	return map[string]any{"id": "fake-" + r.Name, "name": r.Name, "size": r.Size}, nil
}

func (r *incrementalResource) Read(_ context.Context, _ any, prior any) (any, error) {
	return prior, nil
}

func (r *incrementalResource) Update(
	_ context.Context, _ any, _ Prior[incrementalResource, any],
) (any, error) {
	if r.Size == 99 {
		return nil, errIncrementalResource
	}
	atomic.AddInt64(&r.counters.updates, 1)
	return map[string]any{"id": "fake-" + r.Name, "name": r.Name, "size": r.Size}, nil
}

func (r *incrementalResource) Delete(_ context.Context, _ any, _ any) error {
	if r.Name == "fail-delete" {
		return errIncrementalResource
	}
	atomic.AddInt64(&r.counters.deletes, 1)
	return nil
}

func (r *incrementalResource) ReplaceFields() []string {
	return []string{"name"}
}

func (r *incrementalResource) SchemaVersion() int { return 1 }

func incrementalModules(c *incrementalResourceCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"inc": MakeResourceWith[incrementalResource, any](
					func() *incrementalResource {
						return &incrementalResource{counters: c}
					},
				),
			},
		},
	}
}

func incrementalEntry(address, name string, size int64) *state.Entry {
	return &state.Entry{
		Address:       address,
		Type:          state.EntryLeaf,
		Kind:          "inc",
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": name, "size": size},
		Outputs: map[string]any{
			"id":   "fake-" + name,
			"name": name,
			"size": size,
		},
	}
}

func requireIncrementalOutputs(t *testing.T, ent *state.Entry, name string, size int64) {
	t.Helper()
	require.NotNil(t, ent)
	require.Equal(t, "fake-"+name, ent.Outputs["id"])
	require.Equal(t, name, ent.Outputs["name"])
	require.EqualValues(t, size, ent.Outputs["size"])
}

func seedIncrementalState(t *testing.T, store *localstate.LocalStore, entries ...*state.Entry) {
	t.Helper()
	snap := state.NewSnapshot(state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		store.Stack())
	snap.Entries = entries
	rev, err := store.Write(snap)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))
}

func applyIncrementalPlan(
	t *testing.T,
	store *localstate.LocalStore,
	counters *incrementalResourceCounters,
	src string,
) error {
	t.Helper()
	libs := incrementalModules(counters)
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	_, err := planAndApply(exec)
	return err
}

func TestPlanDestroyTearsDownEverything(t *testing.T) {
	rec := &deleteOrder{}
	libs := orderModules(rec)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	src := `
resources: {
  core: {
    thing: {
      a: { name: 'a' }
      b: { name: 'b', dep: resource.core.thing.a.id }
    }
  }
}
outputs: {
  a-id: { value: resource.core.thing.a.id }
}
`
	build := func(destroy bool) *Executor {
		return &Executor{
			DAG:       BuildDAG(parseStack(t, src), libs),
			Libraries: libs,
			Store:     store,
			Factory:   stack,
			Destroy:   destroy,
		}
	}
	_, err := planAndApply(build(false))
	require.NoError(t, err)

	// A destroy plan against the same source tears everything down in
	// reverse dependency order and evaluates no outputs.
	res, err := planAndApply(build(true))
	require.NoError(t, err)
	require.Equal(t, []string{"b", "a"}, rec.order)
	require.Empty(t, res.Outputs)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Empty(t, snap.Entries)
}

func applyStack(
	t *testing.T, store *localstate.LocalStore, libs map[string]*Library,
	src string, inputs map[string]any,
) *ExecResult {
	t.Helper()
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Inputs:    inputs,
		Store:     store,
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	res, err := planAndApply(exec)
	require.NoError(t, err)
	return res
}

func destroyStack(
	t *testing.T, store *localstate.LocalStore, libs map[string]*Library, src string,
) (*ExecResult, error) {
	t.Helper()
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Destroy:   true,
	}
	return planAndApply(exec)
}

func requireEmptyState(t *testing.T, store *localstate.LocalStore) {
	t.Helper()
	snap, err := store.Current()
	require.NoError(t, err)
	require.Empty(t, snap.Entries)
}

func TestPlanDestroyMarksEveryEntryForDeletion(t *testing.T) {
	rec := &deleteOrder{}
	libs := orderModules(rec)
	store := newStateStore(t)
	src := `
resources: {
  core: {
    thing: {
      a: { name: 'a' }
      b: { name: 'b' }
      c: { name: 'c' }
    }
  }
}
`
	applyStack(t, store, libs, src, nil)

	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Destroy:   true,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.True(t, plan.Destroy)
	require.Len(t, plan.Steps, 3)
	for _, s := range plan.Steps {
		require.Equal(t, DecisionDestroy, s.Decision, s.Address)
	}
}

func TestPlanDestroyWithNoPriorStateIsEmpty(t *testing.T) {
	rec := &deleteOrder{}
	libs := orderModules(rec)
	store := newStateStore(t)
	src := `resources: { core: { thing: { a: { name: 'a' } } } }`

	res, err := destroyStack(t, store, libs, src)
	require.NoError(t, err)
	require.Empty(t, rec.order)
	require.Empty(t, res.Outputs)
	requireEmptyState(t, store)
}

func TestDestroyChainDeletesInReverseOrder(t *testing.T) {
	src := `
resources: {
  core: {
    thing: {
      a: { name: 'a' }
      b: { name: 'b', dep: resource.core.thing.a.id }
      c: { name: 'c', dep: resource.core.thing.b.id }
    }
  }
}
`
	// Run the whole cycle several times; the reverse order must be
	// byte-identical every time, not just usually.
	for range 10 {
		rec := &deleteOrder{}
		libs := orderModules(rec)
		store := newStateStore(t)
		applyStack(t, store, libs, src, nil)
		_, err := destroyStack(t, store, libs, src)
		require.NoError(t, err)
		require.Equal(t, []string{"c", "b", "a"}, rec.order)
		requireEmptyState(t, store)
	}
}

func TestDestroyForEachDeletesAllInstances(t *testing.T) {
	rec := &deleteOrder{}
	libs := orderModules(rec)
	store := newStateStore(t)
	src := `
resources: {
  core: {
    thing: {
      many: {
        @for-each: var.names
        name:      @each.key
      }
    }
  }
}
`
	applyStack(t, store, libs, src,
		map[string]any{"names": map[string]any{"alpha": "a", "beta": "b"}})
	_, err := destroyStack(t, store, libs, src)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"alpha", "beta"}, rec.order)
	requireEmptyState(t, store)
}

func TestDestroyComposite(t *testing.T) {
	rec := &deleteOrder{}
	libs := orderModules(rec)
	composite := parseStack(t, `
resources: {
  core: {
    thing: {
      one: { name: var.name }
    }
  }
}
outputs: {
  id: { value: resource.core.thing.one.id }
}
`)
	libs["w"] = &Library{
		Name:               "w",
		ResourceComposites: map[string]*CompositeType{"box": {Name: "box", Body: composite}},
	}
	store := newStateStore(t)
	src := `resources: { w: { box: { x: { name: 'alpha' } } } }`
	applyStack(t, store, libs, src, nil)

	// Before destroy there is the library-call record plus its internal leaf.
	snap, err := store.Current()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 2)

	_, err = destroyStack(t, store, libs, src)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha"}, rec.order)
	requireEmptyState(t, store)
}

func TestDestroyRemovesActionWithoutRunningIt(t *testing.T) {
	rec := &deleteOrder{}
	var runs int64
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[orderResource, any](
					func() *orderResource { return &orderResource{rec: rec} },
				),
			},
			Actions: map[string]ActionRegistration{
				"echo": MakeActionWith[countingAction, any](
					func() *countingAction { return &countingAction{runs: &runs} },
				),
			},
		},
	}
	store := newStateStore(t)
	src := `
resources: { core: { thing: { r: { name: 'r' } } } }
actions: { core: { echo: { e: { echo: 'hi' } } } }
`
	applyStack(t, store, libs, src, nil)
	require.Equal(t, int64(1), atomic.LoadInt64(&runs))

	_, err := destroyStack(t, store, libs, src)
	require.NoError(t, err)
	require.Equal(t, int64(1), atomic.LoadInt64(&runs),
		"an action has no delete step and must not run during destroy")
	require.Equal(t, []string{"r"}, rec.order)
	requireEmptyState(t, store)
}

func TestPlanFileRoundTripsDestroyFlag(t *testing.T) {
	rec := &deleteOrder{}
	libs := orderModules(rec)
	store := newStateStore(t)
	src := `resources: { core: { thing: { a: { name: 'a' } } } }`
	applyStack(t, store, libs, src, nil)

	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Destroy:   true,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)
	require.True(t, pf.Destroy)
	require.Len(t, pf.Steps, 1)
	require.Equal(t, DecisionDestroy, pf.Steps[0].Decision)
}

func TestDestroyClearsActionAndLibraryCallRecords(t *testing.T) {
	compositeBody := parseStack(t, `
inputs: { msg: { type: string } }
actions: { core: { echo: { inner: { echo: var.msg } } } }
outputs: { said: { value: action.core.echo.inner.echo } }
`)
	libs := testModules()
	libs["w"] = &Library{
		Name:               "w",
		ResourceComposites: map[string]*CompositeType{"box": {Name: "box", Body: compositeBody}},
	}
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	// No leaf resources: only a root action, a library-call record, and
	// the composite's internal action. This is the shape that used to
	// plan as "No changes".
	src := `
actions: { core: { echo: { top: { echo: 'hi' } } } }
resources: { w: { box: { x: { msg: 'wrapped' } } } }
`
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	_, err := planAndApply(exec)
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	require.NotEmpty(t, snap.Entries)

	destroyer := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
		Destroy:   true,
	}
	plan, err := destroyer.Plan(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, plan.Steps,
		"destroy must plan to remove action and library-call records, not report no changes")
	for _, s := range plan.Steps {
		require.Equal(t, DecisionDestroy, s.Decision, s.Address)
	}

	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)
	_, err = destroyer.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	requireEmptyState(t, store)
}

func TestDestroySkipsDeleteForAlreadyGoneResource(t *testing.T) {
	var c resourceCounters
	libs := resourceModules(&c)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	src := `resources: { core: { thing: { a: { name: 'a', size: 1 } } } }`

	create := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	_, err := planAndApply(create)
	require.NoError(t, err)

	// The resource vanishes out of band; its read now reports it gone.
	c.readFn = func(any) (any, error) { return nil, ErrNotFound }

	destroyer := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
		Destroy:   true,
	}
	plan, err := destroyer.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Steps, 1)
	require.Equal(t, DecisionDestroy, plan.Steps[0].Decision)
	require.True(t, plan.Steps[0].AlreadyGone, "a read that comes back gone marks the step")

	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)
	_, err = destroyer.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, int64(0), atomic.LoadInt64(&c.deletes),
		"an already-gone resource is dropped from state without a delete")
	requireEmptyState(t, store)
}

func TestApplyPersistsDependsOn(t *testing.T) {
	src := `
resources: {
  core: {
    thing: {
      base:      { name: 'base', size: 1 }
      dependent: { name: resource.core.thing.base.id, size: 2 }
    }
  }
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	_, err := planAndApply(exec)
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	byAddr := map[string]*state.Entry{}
	for _, ent := range snap.Entries {
		byAddr[ent.Address] = ent
	}
	require.Contains(t, byAddr, "resource.core.thing.base")
	require.Contains(t, byAddr, "resource.core.thing.dependent")
	require.Equal(t, []string{"resource.core.thing.base"},
		byAddr["resource.core.thing.dependent"].DependsOn)
	require.Empty(t, byAddr["resource.core.thing.base"].DependsOn)
}

func TestApplyPlanForEachResource(t *testing.T) {
	src := `
resources: {
  core: {
    thing: {
      many: {
        @for-each: var.configs
        name:      @each.key
        size:      @each.value
      }
    }
  }
}
outputs: {
  alpha-id: { value: resource.core.thing.many['alpha'].id }
  beta-id:  { value: resource.core.thing.many['beta'].id }
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Inputs:    map[string]any{"configs": map[string]any{"alpha": int64(1), "beta": int64(2)}},
		Store:     store,
		Factory:   stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)

	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	res, err := exec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, int64(2), atomic.LoadInt64(&c.creates))
	require.Equal(t, "fake-alpha", res.Outputs["alpha-id"])
	require.Equal(t, "fake-beta", res.Outputs["beta-id"])

	snap, err := store.Current()
	require.NoError(t, err)
	addrs := map[string]bool{}
	for _, ent := range snap.Entries {
		addrs[ent.Address] = true
	}
	require.True(t, addrs["resource.core.thing.many['alpha']"])
	require.True(t, addrs["resource.core.thing.many['beta']"])
}

func TestApplyPlanForEachAction(t *testing.T) {
	src := `
actions: {
  core: {
    echo: {
      many: {
        @for-each: var.names
        echo:      @each.value
      }
    }
  }
}
outputs: {
  alpha-said: { value: action.core.echo.many['alpha'].echo }
  beta-said:  { value: action.core.echo.many['beta'].echo }
}
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := testModules()
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Inputs: map[string]any{
			"names": map[string]any{"alpha": "hello-alpha", "beta": "hello-beta"},
		},
		Store:   store,
		Factory: stack,
	}
	res, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, "hello-alpha", res.Outputs["alpha-said"])
	require.Equal(t, "hello-beta", res.Outputs["beta-said"])

	snap, err := store.Current()
	require.NoError(t, err)
	addrs := map[string]bool{}
	for _, ent := range snap.Entries {
		addrs[ent.Address] = true
	}
	require.True(t, addrs["action.core.echo.many['alpha']"])
	require.True(t, addrs["action.core.echo.many['beta']"])
}

func TestApplyPlanForEachActionSkipsUnchanged(t *testing.T) {
	src := `
actions: {
  core: {
    echo: {
      many: {
        @for-each: var.names
        echo:      @each.value
      }
    }
  }
}
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	var runs int64
	libs := countingModules(&runs)
	inputs := map[string]any{
		"names": map[string]any{"alpha": "first", "beta": "second"},
	}
	apply := func() {
		applyOnce(t, &Executor{
			DAG:       BuildDAG(parseStack(t, src), libs),
			Libraries: libs,
			Inputs:    inputs,
			Store:     store,
			Factory:   stack,
		})
	}
	apply()
	require.Equal(t, int64(2), atomic.LoadInt64(&runs))
	apply()
	require.Equal(t, int64(2), atomic.LoadInt64(&runs),
		"second apply skips both instances because their trigger hashes match")
}

func TestApplyPlanForEachData(t *testing.T) {
	src := `
data: {
  core: {
    lookup: {
      many: {
        @for-each: var.keys
        key:       @each.value
      }
    }
  }
}
outputs: {
  alpha-value: { value: data.core.lookup.many['alpha'].value }
  beta-value:  { value: data.core.lookup.many['beta'].value }
}
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := testModules()
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Inputs: map[string]any{
			"keys": map[string]any{"alpha": "alpha-key", "beta": "beta-key"},
		},
		Store:   store,
		Factory: stack,
	}
	res, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, "looked-up:alpha-key", res.Outputs["alpha-value"])
	require.Equal(t, "looked-up:beta-key", res.Outputs["beta-value"])
}

func TestApplyPlanCreatesResource(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
outputs: {
  id: { value: resource.core.thing.one.id }
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)

	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)

	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	res, err := exec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, "fake-alpha", res.Outputs["id"])
	require.Equal(t, int64(1), c.creates)
}

func TestApplyPlanPersistsCreateBeforeLaterFailure(t *testing.T) {
	src := `
resources: {
  core: {
    inc: {
      first: { name: 'first', size: 1 }
      later: {
        @depends-on: [resource.core.inc.first]
        name:        'fail-create'
        size:        1
      }
    }
  }
}
`
	store := newStateStore(t)
	var c incrementalResourceCounters

	err := applyIncrementalPlan(t, store, &c, src)
	require.ErrorIs(t, err, errIncrementalResource)

	snap, err := store.Current()
	require.NoError(t, err)
	first := snap.Find("resource.core.inc.first")
	requireIncrementalOutputs(t, first, "first", 1)
	require.Nil(t, snap.Find("resource.core.inc.later"))
}

func TestApplyPlanPersistsUpdateBeforeLaterFailure(t *testing.T) {
	src := `
resources: {
  core: {
    inc: {
      first: { name: 'first', size: 2 }
      later: {
        @depends-on: [resource.core.inc.first]
        name:        'fail-create'
        size:        1
      }
    }
  }
}
`
	store := newStateStore(t)
	seedIncrementalState(t, store,
		incrementalEntry("resource.core.inc.first", "first", 1))
	var c incrementalResourceCounters

	err := applyIncrementalPlan(t, store, &c, src)
	require.ErrorIs(t, err, errIncrementalResource)

	snap, err := store.Current()
	require.NoError(t, err)
	first := snap.Find("resource.core.inc.first")
	requireIncrementalOutputs(t, first, "first", 2)
	require.Equal(t, "first", first.Inputs["name"])
	require.EqualValues(t, 2, first.Inputs["size"])
	require.Nil(t, snap.Find("resource.core.inc.later"))
}

func TestApplyPlanPersistsReplaceBeforeLaterFailure(t *testing.T) {
	src := `
resources: {
  core: {
    inc: {
      first: { name: 'new', size: 1 }
      later: {
        @depends-on: [resource.core.inc.first]
        name:        'fail-create'
        size:        1
      }
    }
  }
}
`
	store := newStateStore(t)
	seedIncrementalState(t, store,
		incrementalEntry("resource.core.inc.first", "old", 1))
	var c incrementalResourceCounters

	err := applyIncrementalPlan(t, store, &c, src)
	require.ErrorIs(t, err, errIncrementalResource)

	snap, err := store.Current()
	require.NoError(t, err)
	first := snap.Find("resource.core.inc.first")
	requireIncrementalOutputs(t, first, "new", 1)
	require.Equal(t, "new", first.Inputs["name"])
	require.EqualValues(t, 1, first.Inputs["size"])
	require.Nil(t, snap.Find("resource.core.inc.later"))
}

func TestApplyPlanPersistsDestroyBeforeLaterFailure(t *testing.T) {
	src := `description: 'empty'`
	store := newStateStore(t)
	seedIncrementalState(t, store,
		incrementalEntry("resource.core.inc.orphan", "orphan", 1),
		incrementalEntry("resource.core.inc.later", "fail-delete", 1))
	var c incrementalResourceCounters

	err := applyIncrementalPlan(t, store, &c, src)
	require.ErrorIs(t, err, errIncrementalResource)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Nil(t, snap.Find("resource.core.inc.orphan"))
	require.NotNil(t, snap.Find("resource.core.inc.later"))
}

func TestApplyPlanForEachComposite(t *testing.T) {
	composite := parseStack(t, `
inputs: {
  name: { type: string }
  size: { type: integer }
}
resources: {
  core: { thing: { only: { name: var.name, size: var.size } } }
}
outputs: {
  id: { value: resource.core.thing.only.id }
}
`)
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": {Name: "box", Body: composite, Libraries: libs},
		},
	}
	src := `
resources: {
  w: {
    box: {
      many: {
        @for-each: var.configs
        name:      @each.key
        size:      @each.value
      }
    }
  }
}
outputs: {
  alpha-id: { value: resource.w.box.many['alpha'].id }
  beta-id:  { value: resource.w.box.many['beta'].id }
}
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Inputs: map[string]any{
			"configs": map[string]any{"alpha": int64(1), "beta": int64(2)},
		},
		Store:   store,
		Factory: stack,
	}
	res, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, int64(2), atomic.LoadInt64(&c.creates),
		"each instance creates its own leaf")
	require.Equal(t, "fake-alpha", res.Outputs["alpha-id"])
	require.Equal(t, "fake-beta", res.Outputs["beta-id"])

	snap, err := store.Current()
	require.NoError(t, err)
	addrs := map[string]state.EntryType{}
	for _, ent := range snap.Entries {
		addrs[ent.Address] = ent.Type
	}
	require.Equal(t, state.EntryLibraryCall, addrs["resource.w.box.many['alpha']"])
	require.Equal(t, state.EntryLibraryCall, addrs["resource.w.box.many['beta']"])
	require.Equal(t, state.EntryLeaf, addrs["resource.w.box.many['alpha']/resource.core.thing.only"])
	require.Equal(t, state.EntryLeaf, addrs["resource.w.box.many['beta']/resource.core.thing.only"])
}

func TestApplyPlanForEachCompositeOrphan(t *testing.T) {
	composite := parseStack(t, `
inputs: {
  name: { type: string }
}
resources: {
  core: { thing: { only: { name: var.name, size: 1 } } }
}
`)
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": {Name: "box", Body: composite, Libraries: libs},
		},
	}
	src := `
resources: {
  w: {
    box: {
      many: {
        @for-each: var.configs
        name:      @each.key
      }
    }
  }
}
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	apply := func(configs map[string]any) {
		applyOnce(t, &Executor{
			DAG:       BuildDAG(parseStack(t, src), libs),
			Libraries: libs,
			Inputs:    map[string]any{"configs": configs},
			Store:     store,
			Factory:   stack,
		})
	}
	apply(map[string]any{"alpha": true, "beta": true})
	require.Equal(t, int64(2), atomic.LoadInt64(&c.creates))
	apply(map[string]any{"alpha": true})
	require.Equal(t, int64(1), atomic.LoadInt64(&c.deletes),
		"the beta instance's internal leaf is destroyed when the key is removed")

	snap, err := store.Current()
	require.NoError(t, err)
	addrs := map[string]bool{}
	for _, ent := range snap.Entries {
		addrs[ent.Address] = true
	}
	require.True(t, addrs["resource.w.box.many['alpha']"])
	require.True(t, addrs["resource.w.box.many['alpha']/resource.core.thing.only"])
	require.False(t, addrs["resource.w.box.many['beta']"])
	require.False(t, addrs["resource.w.box.many['beta']/resource.core.thing.only"])
}

func TestApplyPlanComposite(t *testing.T) {
	composite := parseStack(t, `
resources: {
  core: { thing: { one: { name: var.name, size: 1 } } }
}
outputs: {
  id: { value: resource.core.thing.one.id }
}
`)
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": {Name: "box", Body: composite},
		},
	}
	src := `
resources: {
  w: { box: { x: { name: 'alpha' } } }
}
outputs: {
  out: { value: resource.w.box.x.id }
}
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	res, err := exec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, "fake-alpha", res.Outputs["out"])
	require.Equal(t, int64(1), c.creates)

	snap, err := store.Current()
	require.NoError(t, err)
	addresses := make([]string, len(snap.Entries))
	types := make([]state.EntryType, len(snap.Entries))
	for i, e := range snap.Entries {
		addresses[i] = e.Address
		types[i] = e.Type
	}
	require.ElementsMatch(t,
		[]string{"resource.w.box.x", "resource.w.box.x/resource.core.thing.one"},
		addresses)
	require.Contains(t, types, state.EntryLibraryCall)
	require.Contains(t, types, state.EntryLeaf)
}

func TestApplyPlanNestedComposite(t *testing.T) {
	clusterBody := parseStack(t, `
inputs: {
  path: { type: string }
}

resources: {
  core: { thing: { x: { name: var.path, size: 1 } } }
}

outputs: {
  path: { value: resource.core.thing.x.name }
}
`)
	layerBody := parseStack(t, `
inputs: {
  target: { type: string }
}

resources: {
  inner-lib: {
    cluster: { only: { path: var.target } }
  }
}

outputs: {
  path: { value: resource.inner-lib.cluster.only.path }
}
`)
	var c resourceCounters
	libs := resourceModules(&c)
	libs["outer-lib"] = &Library{
		Name: "outer-lib",
		ResourceComposites: map[string]*CompositeType{
			"layer": {Name: "layer", Body: layerBody},
		},
	}
	libs["inner-lib"] = &Library{
		Name: "inner-lib",
		ResourceComposites: map[string]*CompositeType{
			"cluster": {Name: "cluster", Body: clusterBody},
		},
	}
	src := `
resources: {
  outer-lib: { layer: { mine: { target: 'alpha' } } }
}
outputs: {
  out: { value: resource.outer-lib.layer.mine.path }
}
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	planExec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	plan, err := planExec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	// Apply runs without root inputs, mirroring the stack binary's
	// `apply` subcommand which only reads the plan file. Both
	// composite scopes (outer and inner) must be seeded from the plan
	// steps so the deepest leaf can read its `var.path`.
	applyExec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	res, err := applyExec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, "alpha", res.Outputs["out"])
	require.Equal(t, int64(1), c.creates)

	snap, err := store.Current()
	require.NoError(t, err)
	addresses := []string{}
	for _, e := range snap.Entries {
		addresses = append(addresses, e.Address)
	}
	require.ElementsMatch(t,
		[]string{
			"resource.outer-lib.layer.mine",
			"resource.outer-lib.layer.mine/resource.inner-lib.cluster.only",
			"resource.outer-lib.layer.mine/resource.inner-lib.cluster.only/resource.core.thing.x",
		},
		addresses,
		"both boundaries persist as library-call records, plus the deepest leaf")
}

func TestApplyPlanCompositeOrphan(t *testing.T) {
	composite := parseStack(t, `
resources: {
  core: {
    thing: {
      one: { name: var.name, size: 1 }
      two: { name: var.name, size: 2 }
    }
  }
}
`)
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": {Name: "box", Body: composite},
		},
	}
	first := `
resources: {
  core: { thing: { keep: { name: 'kept', size: 7 } } }
  w:    { box:   { x:    { name: 'alpha' } } }
}
`
	second := `
resources: {
  core: { thing: { keep: { name: 'kept', size: 7 } } }
}
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	planAndApply := func(src string) *Plan {
		exec := &Executor{
			DAG:       BuildDAG(parseStack(t, src), libs),
			Libraries: libs,
			Store:     store,
			Factory:   stack,
		}
		plan, err := exec.Plan(context.Background())
		require.NoError(t, err)
		encoded, err := EncodePlan(plan)
		require.NoError(t, err)
		pf, err := DecodePlan(encoded)
		require.NoError(t, err)
		_, err = exec.ApplyPlan(context.Background(), pf)
		require.NoError(t, err)
		return plan
	}

	planAndApply(first)
	require.Equal(t, int64(3), c.creates,
		"first apply creates two internals plus one root resource")
	require.Equal(t, int64(0), c.deletes)

	plan := planAndApply(second)
	require.Equal(t, int64(2), c.deletes,
		"both internals are destroyed when the call site goes away")

	destroyed := []string{}
	for _, step := range plan.Steps {
		if step.Decision == DecisionDestroy {
			destroyed = append(destroyed, step.Address)
		}
	}
	require.ElementsMatch(t,
		[]string{
			"resource.w.box.x/resource.core.thing.one",
			"resource.w.box.x/resource.core.thing.two",
		},
		destroyed,
		"the plan reports both internals as destroys")

	snap, err := store.Current()
	require.NoError(t, err)
	addresses := []string{}
	for _, e := range snap.Entries {
		addresses = append(addresses, e.Address)
	}
	require.Equal(t, []string{"resource.core.thing.keep"}, addresses,
		"only the root-level resource that stays in source remains in state")
}

func TestApplyPlanCompositeWithRootVarArgs(t *testing.T) {
	// The plan and apply phases run separately and apply does not
	// have access to the root inputs that plan used. The composite
	// boundary's args are evaluated at plan time and must seed the
	// composite scope at apply time so internals can read them.
	composite := parseStack(t, `
inputs: {
  who: { type: string }
}
resources: {
  core: { thing: { greet: { name: var.who, size: 1 } } }
}
`)
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"hello": {Name: "hello", Body: composite},
		},
	}
	src := `
inputs: {
  who: { type: string }
}
resources: {
  w: { hello: { x: { who: var.who } } }
}
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	planExec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Inputs:    map[string]any{"who": "world"},
		Store:     store,
		Factory:   stack,
	}
	plan, err := planExec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	// Apply runs without root inputs, mirroring the stack binary's
	// `apply` subcommand which reads only the plan file.
	applyExec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	_, err = applyExec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, int64(1), c.creates)

	snap, err := store.Current()
	require.NoError(t, err)
	leaf := findEntryByAddr(snap, "resource.w.hello.x/resource.core.thing.greet")
	require.NotNil(t, leaf)
	require.Equal(t, "world", leaf.Inputs["name"])
}

func findEntryByAddr(snap *state.Snapshot, addr string) *state.Entry {
	for _, e := range snap.Entries {
		if e.Address == addr {
			return e
		}
	}
	return nil
}

func TestApplyPlanCompositeUpdateInPlace(t *testing.T) {
	composite := parseStack(t, `
resources: {
  core: { thing: { one: { name: var.name, size: 1 } } }
}
`)
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": {Name: "box", Body: composite},
		},
	}
	first := `
resources: {
  w: { box: { x: { name: 'alpha' } } }
}
`
	second := `
resources: {
  w: { box: { x: { name: 'alpha' } } }
}
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	planAndApply := func(src string) {
		exec := &Executor{
			DAG:       BuildDAG(parseStack(t, src), libs),
			Libraries: libs,
			Store:     store,
			Factory:   stack,
		}
		plan, err := exec.Plan(context.Background())
		require.NoError(t, err)
		encoded, err := EncodePlan(plan)
		require.NoError(t, err)
		pf, err := DecodePlan(encoded)
		require.NoError(t, err)
		_, err = exec.ApplyPlan(context.Background(), pf)
		require.NoError(t, err)
	}

	planAndApply(first)
	require.Equal(t, int64(1), c.creates)
	planAndApply(second)
	require.Equal(t, int64(1), c.creates,
		"unchanged composite call site does not recreate internals")
	require.Equal(t, int64(0), c.deletes)
	require.Equal(t, int64(0), c.updates)
}

func TestApplyPlanRefusesOnStateRevDrift(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	// Drift: a separate apply changes state out from under our plan.
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	_, err = exec.ApplyPlan(context.Background(), pf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "state-rev drift")
}

func TestApplyPlanWaitsForLock(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)
	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	held, err := store.Lock(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Unlock() })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = exec.ApplyPlan(ctx, pf)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestApplyPlanRefusesOnStackMismatch(t *testing.T) {
	src := `description: 'x'`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), nil),
		Libraries: map[string]*Library{},
		Store:     store,
		Factory:   stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	// Apply against a different stack identity.
	exec.Factory = state.FactoryInfo{Name: "different", Version: "v0", ContentRevision: "c0"}
	_, err = exec.ApplyPlan(context.Background(), pf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "different")
}

func TestEncodeDecodePlan(t *testing.T) {
	plan := &Plan{
		Factory:  state.FactoryInfo{Name: "x", Version: "v1", ContentRevision: "abc"},
		Stack:    "prod",
		StateRev: "2026-05-01T00:00:00.000000000Z",
		Steps: []*PlanStep{
			{
				Address:  "resource.core.thing.x",
				Kind:     NodeResource,
				Decision: DecisionCreate,
				Inputs:   map[string]any{"name": "x"},
			},
		},
	}
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)
	require.Equal(t, plan.Factory.Name, pf.Factory.Name)
	require.Equal(t, plan.StateRev, pf.StateRev)
	require.Equal(t, "resource.core.thing.x", pf.Steps[0].Address)
	require.Equal(t, DecisionCreate, pf.Steps[0].Decision)
}

func TestActionRerunsWhenTriggerSourceChanges(t *testing.T) {
	src := func(name string) string {
		return `
resources: {
  core: { thing: { one: { name: '` + name + `', size: 1 } } }
}
actions: {
  core: {
    echo: {
      observe: {
        @trigger: resource.core.thing.one.id
        echo:     'observed'
      }
    }
  }
}
`
	}

	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	var resCounters resourceCounters
	var actionRuns int64
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[countingResource, any](
					func() *countingResource {
						return &countingResource{counters: &resCounters}
					},
				),
			},
			Actions: map[string]ActionRegistration{
				"echo": MakeActionWith[countingAction, any](
					func() *countingAction {
						return &countingAction{runs: &actionRuns}
					},
				),
			},
		},
	}

	planAndApply := func(stackSrc string) {
		exec := &Executor{
			DAG:       BuildDAG(parseStack(t, stackSrc), libs),
			Libraries: libs,
			Store:     store,
			Factory:   stack,
		}
		plan, err := exec.Plan(context.Background())
		require.NoError(t, err)
		encoded, err := EncodePlan(plan)
		require.NoError(t, err)
		pf, err := DecodePlan(encoded)
		require.NoError(t, err)
		_, err = exec.ApplyPlan(context.Background(), pf)
		require.NoError(t, err)
	}

	// First run: fresh state. Resource is created; action runs even though
	// its trigger references a not-yet-created resource.
	planAndApply(src("alpha"))
	require.Equal(t, int64(1), atomic.LoadInt64(&actionRuns))

	// Second run, same source: resource is unchanged, action should skip.
	planAndApply(src("alpha"))
	require.Equal(t, int64(1), atomic.LoadInt64(&actionRuns),
		"action should skip on the second run when upstream is unchanged")

	// Third run with the resource's name changed: ReplaceFields=["name"]
	// triggers a replace, which the action treats as a rerun signal.
	planAndApply(src("beta"))
	require.Equal(t, int64(2), atomic.LoadInt64(&actionRuns),
		"action should rerun when its upstream resource is changing")
}

func TestDecodePlanRejectsBadFormatVersion(t *testing.T) {
	bad := []byte(`{"format-version": 99, "stack": {"name": "x"}, "steps": []}`)
	_, err := DecodePlan(bad)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported format-version")
}
