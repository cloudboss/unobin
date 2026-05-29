package runtime

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudboss/unobin/pkg/envencrypt"
	"github.com/cloudboss/unobin/pkg/localstate"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

type echoAction struct {
	Echo string
}

func (a *echoAction) Run(_ context.Context, _ any) (any, error) {
	return map[string]any{"echo": a.Echo, "len": int64(len(a.Echo))}, nil
}

type lookupDataSource struct {
	Key string
}

func (d *lookupDataSource) Read(_ context.Context, _ any) (any, error) {
	return map[string]any{"value": "looked-up:" + d.Key}, nil
}

type failingAction struct{}

func (failingAction) Run(_ context.Context, _ any) (any, error) {
	return nil, errors.New("intentional failure")
}

func testModules() map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Actions: map[string]ActionRegistration{
				"echo": MakeAction[echoAction, any](),
				"fail": MakeAction[failingAction, any](),
			},
			DataSources: map[string]DataSourceRegistration{
				"lookup": MakeDataSource[lookupDataSource, any](),
			},
			Functions: map[string]FunctionType{
				"uppercase": {
					Name: "uppercase",
					Func: func(args []any) (any, error) {
						s, ok := args[0].(string)
						if !ok {
							return nil, fmt.Errorf("uppercase: want string, got %T", args[0])
						}
						return strings.ToUpper(s), nil
					},
				},
			},
		},
	}
}

func runExecutor(t *testing.T, src string, inputs map[string]any) (*ExecResult, error) {
	t.Helper()
	f := parseStack(t, src)
	libs := testModules()
	g := BuildDAG(f, libs)
	exec := &Executor{
		DAG:       g,
		Libraries: libs,
		Inputs:    inputs,
		Store:     newStateStore(t),
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	return planAndApply(exec)
}

func TestExecutorRequiresStore(t *testing.T) {
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, `description: 'x'`), nil),
		Libraries: testModules(),
	}
	_, err := exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Store")
}

func TestExecutorOutputOnly(t *testing.T) {
	res, err := runExecutor(t, `
outputs: {
  region: { value: var.region }
}
`, map[string]any{"region": "us-east-1"})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"region": "us-east-1"}, res.Outputs)
}

func TestExecutorActionRuns(t *testing.T) {
	res, err := runExecutor(t, `
actions: {
  core: {
    echo: {
      hi: { echo: 'hello' }
    }
  }
}
outputs: {
  said:    { value: action.core.echo.hi.echo }
  letters: { value: action.core.echo.hi.len }
}
`, nil)
	require.NoError(t, err)
	require.Equal(t, "hello", res.Outputs["said"])
	require.Equal(t, int64(5), res.Outputs["letters"])
}

func TestExecutorInputFlowsToAction(t *testing.T) {
	res, err := runExecutor(t, `
actions: {
  core: {
    echo: {
      greet: { echo: var.name }
    }
  }
}
outputs: {
  said: { value: action.core.echo.greet.echo }
}
`, map[string]any{"name": "world"})
	require.NoError(t, err)
	require.Equal(t, "world", res.Outputs["said"])
}

func TestExecutorDataSource(t *testing.T) {
	res, err := runExecutor(t, `
data: {
  core: {
    lookup: {
      it: { key: var.key }
    }
  }
}
outputs: {
  found: { value: data.core.lookup.it.value }
}
`, map[string]any{"key": "abc"})
	require.NoError(t, err)
	require.Equal(t, "looked-up:abc", res.Outputs["found"])
}

func TestExecutorActionDependsOnAction(t *testing.T) {
	res, err := runExecutor(t, `
actions: {
  core: {
    echo: {
      first:  { echo: 'one' }
      second: { echo: action.core.echo.first.echo }
    }
  }
}
outputs: {
  result: { value: action.core.echo.second.echo }
}
`, nil)
	require.NoError(t, err)
	require.Equal(t, "one", res.Outputs["result"])
}

func TestExecutorPropagatesActionError(t *testing.T) {
	_, err := runExecutor(t, `
actions: {
  core: {
    fail: {
      f: {}
    }
  }
}
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "action.core.fail.f")
	require.Contains(t, err.Error(), "intentional failure")
}

type resourceCounters struct {
	creates int64
	updates int64
	deletes int64
	// readFn lets a test control what countingResource.Read returns;
	// nil means Read returns prior unchanged (no drift, not gone).
	readFn func(prior any) (any, error)
}

type countingResource struct {
	Name string
	Size int64

	counters *resourceCounters
}

func (r *countingResource) Create(_ context.Context, _ any) (any, error) {
	atomic.AddInt64(&r.counters.creates, 1)
	return map[string]any{"id": "fake-" + r.Name, "name": r.Name, "size": r.Size}, nil
}

func (r *countingResource) Read(_ context.Context, _ any, prior any) (any, error) {
	if r.counters.readFn != nil {
		return r.counters.readFn(prior)
	}
	return prior, nil
}

func (r *countingResource) Update(_ context.Context, _ any, prior any) (any, error) {
	atomic.AddInt64(&r.counters.updates, 1)
	m, _ := prior.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	m["name"] = r.Name
	m["size"] = r.Size
	return m, nil
}

func (r *countingResource) Delete(_ context.Context, _ any, _ any) error {
	atomic.AddInt64(&r.counters.deletes, 1)
	return nil
}

func (r *countingResource) ReplaceFields() []string {
	return []string{"name"}
}

func (r *countingResource) SchemaVersion() int { return 1 }

// countingResourceV2 is countingResource with SchemaVersion bumped
// to 2 and no Migrate, used by plan tests that exercise the
// missing-migration error path.
type countingResourceV2 struct {
	countingResource `ub:",squash"`
}

func (r *countingResourceV2) SchemaVersion() int { return 2 }

// migratingCountingResource is countingResourceV2 with a Migrate
// method that rewrites `id` to `name-id` in state, used by the plan
// test for the migration happy path.
type migratingCountingResource struct {
	countingResource `ub:",squash"`
}

func (r *migratingCountingResource) SchemaVersion() int { return 2 }

func (r *migratingCountingResource) Migrate(_ int, st map[string]any) (map[string]any, error) {
	out := map[string]any{}
	maps.Copy(out, st)
	if v, ok := out["id"]; ok {
		out["name-id"] = v
		delete(out, "id")
	}
	return out, nil
}

func resourceModules(c *resourceCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[countingResource, any](
					func() *countingResource { return &countingResource{counters: c} },
				),
			},
		},
	}
}

func TestExecutorRunsComposite(t *testing.T) {
	composite := parseStack(t, `
resources: {
  core: {
    thing: {
      one: { name: var.name, size: 1 }
    }
  }
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
	res := applyOnce(t, exec)
	require.Equal(t, "fake-alpha", res.Outputs["out"])
	require.Equal(t, int64(1), c.creates)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 2)

	var leaf, libCall *state.Entry
	for _, e := range snap.Entries {
		switch e.Type {
		case state.EntryLeaf:
			leaf = e
		case state.EntryLibraryCall:
			libCall = e
		}
	}
	require.NotNil(t, leaf)
	require.Equal(t, "resource.w.box.x/resource.core.thing.one", leaf.Address)
	require.Equal(t, "thing", leaf.Kind)

	require.NotNil(t, libCall)
	require.Equal(t, "resource.w.box.x", libCall.Address)
	require.Equal(t, "w", libCall.Library)
	require.Equal(t, "box", libCall.LibraryType)
	require.Equal(t, "alpha", libCall.Inputs["name"])
	require.Equal(t, "fake-alpha", libCall.Outputs["id"])
}

func TestExecutorAppliesDataComposite(t *testing.T) {
	composite := parseStack(t, `
data: {
  core: {
    lookup: { it: { key: var.key } }
  }
}
outputs: {
  value: { value: data.core.lookup.it.value }
}
`)
	libs := testModules()
	libs["w"] = &Library{
		Name: "w",
		DataComposites: map[string]*CompositeType{
			"box": {Name: "box", Category: NodeData, Body: composite},
		},
	}
	src := `
data: {
  w: { box: { x: { key: 'abc' } } }
}
outputs: {
  out: { value: data.w.box.x.value }
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
	res := applyOnce(t, exec)
	require.Equal(t, "looked-up:abc", res.Outputs["out"],
		"the data composite's published output flows to the root output")

	snap, err := store.Current()
	require.NoError(t, err)
	var libCall *state.Entry
	for _, e := range snap.Entries {
		if e.Type == state.EntryLibraryCall {
			libCall = e
		}
	}
	require.NotNil(t, libCall, "the data composite call records a library-call entry")
	require.Equal(t, "data.w.box.x", libCall.Address,
		"the boundary address carries the data category root")
	require.Equal(t, "w", libCall.Library)
	require.Equal(t, "box", libCall.LibraryType)
	require.Equal(t, "looked-up:abc", libCall.Outputs["value"])

	// A second plan and apply against the prior state still resolves the
	// boundary's output, so the re-plan path handles a data boundary too.
	res2 := applyOnce(t, exec)
	require.Equal(t, "looked-up:abc", res2.Outputs["out"])
}

func TestExecutorAppliesActionComposite(t *testing.T) {
	composite := parseStack(t, `
actions: {
  core: {
    echo: { it: { echo: var.msg } }
  }
}
outputs: {
  said: { value: action.core.echo.it.echo }
}
`)
	libs := testModules()
	libs["ops"] = &Library{
		Name: "ops",
		ActionComposites: map[string]*CompositeType{
			"greet": {Name: "greet", Category: NodeAction, Body: composite},
		},
	}
	src := `
actions: {
  ops: { greet: { hello: { msg: 'hi' } } }
}
outputs: {
  out: { value: action.ops.greet.hello.said }
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
	res := applyOnce(t, exec)
	require.Equal(t, "hi", res.Outputs["out"],
		"the action composite's published output flows to the root output")

	snap, err := store.Current()
	require.NoError(t, err)
	var libCall *state.Entry
	for _, e := range snap.Entries {
		if e.Type == state.EntryLibraryCall {
			libCall = e
		}
	}
	require.NotNil(t, libCall, "the action composite call records a library-call entry")
	require.Equal(t, "action.ops.greet.hello", libCall.Address,
		"the boundary address carries the action category root")
	require.Equal(t, "hi", libCall.Outputs["said"])
}

func TestExecutorForEachResourceCreatesPerInstance(t *testing.T) {
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
		Inputs: map[string]any{
			"configs": map[string]any{
				"alpha": int64(1),
				"beta":  int64(2),
			},
		},
		Store:   store,
		Factory: stack,
	}
	res := applyOnce(t, exec)
	require.Equal(t, int64(2), atomic.LoadInt64(&c.creates))
	require.Equal(t, "fake-alpha", res.Outputs["alpha-id"])
	require.Equal(t, "fake-beta", res.Outputs["beta-id"])

	snap, err := store.Current()
	require.NoError(t, err)
	addrs := map[string]bool{}
	for _, ent := range snap.Entries {
		addrs[ent.Address] = true
	}
	require.True(t, addrs["resource.core.thing.many['alpha']"], "alpha instance in state")
	require.True(t, addrs["resource.core.thing.many['beta']"], "beta instance in state")
}

func TestExecutorForEachOrphanInstanceDeleted(t *testing.T) {
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
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)
	runOnce := func(configs map[string]any) {
		exec := &Executor{
			DAG:       BuildDAG(parseStack(t, src), libs),
			Libraries: libs,
			Inputs:    map[string]any{"configs": configs},
			Store:     store,
			Factory:   stack,
		}
		applyOnce(t, exec)
	}
	runOnce(map[string]any{"alpha": int64(1), "beta": int64(2)})
	require.Equal(t, int64(2), atomic.LoadInt64(&c.creates))

	runOnce(map[string]any{"alpha": int64(1)})
	require.Equal(t, int64(1), atomic.LoadInt64(&c.deletes), "beta instance destroyed")

	snap, err := store.Current()
	require.NoError(t, err)
	addrs := map[string]bool{}
	for _, ent := range snap.Entries {
		addrs[ent.Address] = true
	}
	require.True(t, addrs["resource.core.thing.many['alpha']"])
	require.False(t, addrs["resource.core.thing.many['beta']"], "beta dropped from state")
}

func TestExecutorForEachRejectsList(t *testing.T) {
	src := `
resources: {
  core: {
    thing: {
      many: {
        @for-each: var.items
        name:      @each.value
      }
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
		Inputs:    map[string]any{"items": []any{"a", "b"}},
		Store:     store,
		Factory:   stack,
	}
	_, err := planAndApply(exec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "@for-each")
}

func TestExecutorModuleFunctionInOutput(t *testing.T) {
	res, err := runExecutor(t, `
outputs: {
  shout: { value: core.uppercase(var.name) }
}
`, map[string]any{"name": "hello"})
	require.NoError(t, err)
	require.Equal(t, "HELLO", res.Outputs["shout"])
}

func TestExecutorModuleFunctionInsideComposite(t *testing.T) {
	layerBody := parseStack(t, `
inputs: { name: { type: string } }
outputs: { shout: { value: core.uppercase(var.name) } }
`)
	rootMods := map[string]*Library{
		"wrapper": {
			Name: "wrapper",
			ResourceComposites: map[string]*CompositeType{
				"layer": {Name: "layer", Body: layerBody, Libraries: testModules()},
			},
		},
	}
	src := `
resources: { wrapper: { layer: { x: { name: 'hi' } } } }
outputs: { out: { value: resource.wrapper.layer.x.shout } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), rootMods),
		Libraries: rootMods,
		Store:     store,
		Factory:   stack,
	}
	res := applyOnce(t, exec)
	require.Equal(t, "HI", res.Outputs["out"])
}

func TestExecutorCompositeUsesItsOwnModules(t *testing.T) {
	// The composite declares which libraries its body uses via its own
	// imports. The runtime should resolve composite-internal lookups
	// against the composite's Libraries table, not the stack root's. This
	// is the encapsulation that lets a composite be reusable without the
	// caller needing to import everything the composite uses transitively.
	layerBody := parseStack(t, `
inputs: {
  name: { type: string }
}

resources: {
  core: { thing: { y: { name: var.name, size: 1 } } }
}

outputs: {
  id: { value: resource.core.thing.y.id }
}
`)
	var c resourceCounters
	// "core" is registered only in the composite's Libraries, never in
	// the stack-root libs.
	composite := &CompositeType{
		Name:      "layer",
		Body:      layerBody,
		Libraries: resourceModules(&c),
	}
	rootMods := map[string]*Library{
		"outer-lib": {
			Name: "outer-lib",
			ResourceComposites: map[string]*CompositeType{
				"layer": composite,
			},
		},
	}
	src := `
resources: {
  outer-lib: { layer: { x: { name: 'alpha' } } }
}
outputs: {
  out: { value: resource.outer-lib.layer.x.id }
}
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), rootMods),
		Libraries: rootMods,
		Store:     store,
		Factory:   stack,
	}
	res, err := planAndApply(exec)
	require.NoError(t, err,
		"composite-internal core.thing should resolve via the composite's own Libraries")
	require.Equal(t, "fake-alpha", res.Outputs["out"])
	require.Equal(t, int64(1), c.creates)
}

func TestExecutorRunsNestedComposite(t *testing.T) {
	clusterBody := parseStack(t, `
inputs: {
  path: { type: string }
}

resources: {
  core: {
    thing: { x: { name: var.path, size: 1 } }
  }
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
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	res := applyOnce(t, exec)
	require.Equal(t, "alpha", res.Outputs["out"],
		"path flows up through both composite layers")
	require.Equal(t, int64(1), c.creates,
		"only the deepest leaf creates a real resource")

	snap, err := store.Current()
	require.NoError(t, err)

	byAddr := map[string]*state.Entry{}
	for _, e := range snap.Entries {
		byAddr[e.Address] = e
	}

	leafAddr := "resource.outer-lib.layer.mine/resource.inner-lib.cluster.only/resource.core.thing.x"
	leaf := byAddr[leafAddr]
	require.NotNil(t, leaf, "deepest leaf persists at fully chained address")
	require.Equal(t, state.EntryLeaf, leaf.Type)

	innerAddr := "resource.outer-lib.layer.mine/resource.inner-lib.cluster.only"
	inner := byAddr[innerAddr]
	require.NotNil(t, inner)
	require.Equal(t, state.EntryLibraryCall, inner.Type)
	require.Equal(t, "inner-lib", inner.Library)
	require.Equal(t, "cluster", inner.LibraryType)

	outerAddr := "resource.outer-lib.layer.mine"
	outer := byAddr[outerAddr]
	require.NotNil(t, outer)
	require.Equal(t, state.EntryLibraryCall, outer.Type)
	require.Equal(t, "outer-lib", outer.Library)
}

func TestExecutorNestedCompositeEncapsulation(t *testing.T) {
	// Inner's leaf produces {id, name, size}; inner only publishes
	// {path}. Outer's outputs reference the boundary's published
	// outputs, not the leaf's internals.
	clusterBody := parseStack(t, `
inputs: {
  path: { type: string }
}

resources: {
  core: {
    thing: { x: { name: var.path, size: 7 } }
  }
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
  exposed: { value: resource.inner-lib.cluster.only.path }
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

	t.Run("only published outputs cross the boundary", func(t *testing.T) {
		src := `
resources: {
  outer-lib: { layer: { mine: { target: 'beta' } } }
}
outputs: {
  out: { value: resource.outer-lib.layer.mine.exposed }
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
		res := applyOnce(t, exec)
		require.Equal(t, "beta", res.Outputs["out"])

		snap, err := store.Current()
		require.NoError(t, err)
		var inner *state.Entry
		for _, e := range snap.Entries {
			if e.Address == "resource.outer-lib.layer.mine/resource.inner-lib.cluster.only" {
				inner = e
			}
		}
		require.NotNil(t, inner)
		require.Equal(t, map[string]any{"path": "beta"}, inner.Outputs,
			"inner boundary's published outputs are exactly the composite's outputs block, "+
				"not the leaf's full output map")
		require.NotContains(t, inner.Outputs, "id",
			"leaf's internal id field must not leak through the boundary")
		require.NotContains(t, inner.Outputs, "size",
			"leaf's internal size field must not leak through the boundary")
	})

	t.Run("non-published fields are unreachable from outer scope", func(t *testing.T) {
		// Outer attempts to reference resource.inner-lib.cluster.only.size
		// which is the leaf's `size` field, not in inner's `outputs:` block.
		// The reference must fail at eval time because outer scope holds
		// only the boundary's published map.
		src := `
resources: {
  outer-lib: { layer: { mine: { target: 'gamma' } } }
}
outputs: {
  leak: { value: resource.outer-lib.layer.mine.size }
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
		_, err := planAndApply(exec)
		require.Error(t, err,
			"outer scope must not expose the inner leaf's internal fields")
		require.Contains(t, err.Error(), "not found")
	})
}

func TestExecutorCompositeInternalDataAndAction(t *testing.T) {
	composite := parseStack(t, `
inputs: {
  key: { type: string }
}
data: {
  core: {
    lookup: { found: { key: var.key } }
  }
}
actions: {
  core: {
    echo: { say: { echo: data.core.lookup.found.value } }
  }
}
outputs: {
  said: { value: action.core.echo.say.echo }
}
`)
	libs := testModules()
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": {Name: "box", Body: composite},
		},
	}
	src := `
resources: {
  w: { box: { x: { key: 'banana' } } }
}
outputs: {
  result: { value: resource.w.box.x.said }
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
	res := applyOnce(t, exec)
	require.Equal(t, "looked-up:banana", res.Outputs["result"])

	snap, err := store.Current()
	require.NoError(t, err)
	var actionEntry, libCall *state.Entry
	for _, e := range snap.Entries {
		switch e.Address {
		case "resource.w.box.x/action.core.echo.say":
			actionEntry = e
		case "resource.w.box.x":
			libCall = e
		}
	}
	require.NotNil(t, actionEntry,
		"internal action should be persisted under composite-prefixed address")
	require.Equal(t, state.EntryAction, actionEntry.Type)
	require.Equal(t, "looked-up:banana", actionEntry.Outputs["echo"])

	require.NotNil(t, libCall)
	require.Equal(t, "looked-up:banana", libCall.Outputs["said"])
}

func TestExecutorCreatesResource(t *testing.T) {
	src := `
resources: {
  core: {
    thing: { one: { name: 'alpha', size: 1 } }
  }
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
	res := applyOnce(t, exec)
	require.Equal(t, "fake-alpha", res.Outputs["id"])
	require.Equal(t, int64(1), atomic.LoadInt64(&c.creates))
	require.Equal(t, int64(0), atomic.LoadInt64(&c.updates))
}

func TestExecutorSameInputsNoCreateOrUpdate(t *testing.T) {
	src := `
resources: {
  core: {
    thing: { one: { name: 'alpha', size: 1 } }
  }
}
`
	var c resourceCounters
	runExecutorTwice(t, src, resourceModules(&c))
	require.Equal(t, int64(1), atomic.LoadInt64(&c.creates))
	require.Equal(t, int64(0), atomic.LoadInt64(&c.updates))
}

func TestExecutorChangedInputsTriggersUpdate(t *testing.T) {
	first := `
resources: {
  core: {
    thing: { one: { name: 'alpha', size: 1 } }
  }
}
`
	second := `
resources: {
  core: {
    thing: { one: { name: 'alpha', size: 9 } }
  }
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)

	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, first), libs), Libraries: libs, Store: store, Factory: stack,
	})
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, second), libs), Libraries: libs, Store: store, Factory: stack,
	})

	require.Equal(t, int64(1), atomic.LoadInt64(&c.creates))
	require.Equal(t, int64(1), atomic.LoadInt64(&c.updates))
}

func TestExecutorReplaceFieldChangeTriggersDeleteAndCreate(t *testing.T) {
	first := `
resources: {
  core: {
    thing: { one: { name: 'alpha', size: 1 } }
  }
}
`
	second := `
resources: {
  core: {
    thing: { one: { name: 'beta', size: 1 } }
  }
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)

	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, first), libs), Libraries: libs, Store: store, Factory: stack,
	})
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, second), libs), Libraries: libs, Store: store, Factory: stack,
	})

	require.Equal(t, int64(2), atomic.LoadInt64(&c.creates),
		"replace destroys the old and creates a new")
	require.Equal(t, int64(1), atomic.LoadInt64(&c.deletes),
		"replace deletes the old before creating")
	require.Equal(t, int64(0), atomic.LoadInt64(&c.updates),
		"replace bypasses Update")
}

func TestExecutorOrphanResourceDeleted(t *testing.T) {
	first := `
resources: {
  core: {
    thing: {
      keep:  { name: 'a', size: 1 }
      orph:  { name: 'b', size: 2 }
    }
  }
}
`
	second := `
resources: {
  core: {
    thing: {
      keep: { name: 'a', size: 1 }
    }
  }
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)

	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, first), libs), Libraries: libs, Store: store, Factory: stack,
	})
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, second), libs), Libraries: libs, Store: store, Factory: stack,
	})

	require.Equal(t, int64(1), atomic.LoadInt64(&c.deletes),
		"the orphan resource (orph) should be deleted on the second run")

	snap, err := store.Current()
	require.NoError(t, err)
	addresses := []string{}
	for _, e := range snap.Entries {
		if e.Type == state.EntryLeaf {
			addresses = append(addresses, e.Address)
		}
	}
	require.Equal(t, []string{"resource.core.thing.keep"}, addresses)
}

func TestExecutorResourceMissingType(t *testing.T) {
	_, err := runExecutor(t, `
resources: {
  core: { not-a-thing: { x: {} } }
}
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not-a-thing")
}

func TestExecutorUnknownModule(t *testing.T) {
	_, err := runExecutor(t, `
actions: {
  unknown: { echo: { x: { echo: 'hi' } } }
}
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")
}

func TestExecutorUnknownActionType(t *testing.T) {
	_, err := runExecutor(t, `
actions: {
  core: { not-a-type: { x: {} } }
}
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not-a-type")
}

func TestExecutorEmptyStack(t *testing.T) {
	res, err := runExecutor(t, `description: 'empty'`, nil)
	require.NoError(t, err)
	require.Empty(t, res.Outputs)
}

type countingAction struct {
	Echo string
	runs *int64
}

func (a *countingAction) Run(_ context.Context, _ any) (any, error) {
	atomic.AddInt64(a.runs, 1)
	return map[string]any{"echo": a.Echo}, nil
}

func newStateStore(t *testing.T) *localstate.LocalStore {
	t.Helper()
	s, err := localstate.NewLocalStore(t.TempDir(), "test-stack", "prod", envencrypt.Noop{})
	require.NoError(t, err)
	return s
}

func runExecutorTwice(
	t *testing.T, src string, libraries map[string]*Library,
) (*ExecResult, *ExecResult) {
	t.Helper()
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g := BuildDAG(parseStack(t, src), libraries)

	first := applyOnce(t, &Executor{DAG: g, Libraries: libraries, Store: store, Factory: stack})
	second := applyOnce(t, &Executor{DAG: g, Libraries: libraries, Store: store, Factory: stack})
	return first, second
}

func countingModules(runs *int64) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Actions: map[string]ActionRegistration{
				"echo": MakeActionWith[countingAction, any](
					func() *countingAction { return &countingAction{runs: runs} },
				),
			},
		},
	}
}

func TestExecutorPersistsSnapshot(t *testing.T) {
	store := newStateStore(t)
	libs := testModules()
	exec := &Executor{
		DAG: BuildDAG(parseStack(t, `
actions: {
  core: {
    echo: { hi: { echo: 'hello' } }
  }
}
`), libs),
		Libraries: libs,
		Store:     store,
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	res := applyOnce(t, exec)
	require.NotEmpty(t, res.WrittenRev)

	gotRev, err := store.CurrentRev()
	require.NoError(t, err)
	require.Equal(t, res.WrittenRev, gotRev)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 1)
	require.Equal(t, "action.core.echo.hi", snap.Entries[0].Address)
	require.Equal(t, state.EntryAction, snap.Entries[0].Type)
	require.NotEmpty(t, snap.Entries[0].TriggerHash)
}

func TestExecutorSkipsActionWhenInputsUnchanged(t *testing.T) {
	src := `
actions: {
  core: {
    echo: { hi: { echo: 'hello' } }
  }
}
`
	var runs int64
	runExecutorTwice(t, src, countingModules(&runs))
	require.Equal(t, int64(1), atomic.LoadInt64(&runs),
		"action should run once across two executions when inputs are unchanged")
}

func TestExecutorAlwaysTriggerReruns(t *testing.T) {
	src := `
actions: {
  core: {
    echo: {
      hi: {
        @trigger: 'always'
        echo:     'hello'
      }
    }
  }
}
`
	var runs int64
	runExecutorTwice(t, src, countingModules(&runs))
	require.Equal(t, int64(2), atomic.LoadInt64(&runs),
		"action with @trigger: 'always' should run on every execution")
}

func TestExecutorExplicitTriggerSkipsWhenSame(t *testing.T) {
	src := `
actions: {
  core: {
    echo: {
      hi: {
        @trigger: 'fixed-key'
        echo:     'hello'
      }
    }
  }
}
`
	var runs int64
	runExecutorTwice(t, src, countingModules(&runs))
	require.Equal(t, int64(1), atomic.LoadInt64(&runs))
}

func TestConfigForUsesNodeAlias(t *testing.T) {
	leaf := &Node{
		Address:       "resource.aws.instance.web",
		Alias:         "aws",
		Configuration: "east2",
	}
	e := &Executor{
		DAG: &DAG{Nodes: map[string]*Node{leaf.Address: leaf}},
		Configurations: map[string]map[string]any{
			"aws": {
				"default": "default-cfg",
				"east2":   "east2-cfg",
			},
		},
	}
	require.Equal(t, "east2-cfg", e.configFor(leaf))
}

func TestConfigForFallsBackToDefault(t *testing.T) {
	leaf := &Node{
		Address: "resource.aws.instance.web",
		Alias:   "aws",
	}
	e := &Executor{
		DAG: &DAG{Nodes: map[string]*Node{leaf.Address: leaf}},
		Configurations: map[string]map[string]any{
			"aws": {"default": "default-cfg"},
		},
	}
	require.Equal(t, "default-cfg", e.configFor(leaf))
}

func TestConfigForPicksUpCompositeRemap(t *testing.T) {
	composite := &Node{
		Address:             "resource.net.cluster.east",
		Kind:                NodeComposite,
		Alias:               "net",
		ConfigurationsRemap: map[string]ConfigRef{"aws": {Alias: "aws", Configuration: "east2"}},
	}
	leaf := &Node{
		Address:   "resource.net.cluster.east/resource.aws.instance.worker",
		Alias:     "aws",
		Composite: composite.Address,
	}
	e := &Executor{
		DAG: &DAG{Nodes: map[string]*Node{
			composite.Address: composite,
			leaf.Address:      leaf,
		}},
		Configurations: map[string]map[string]any{
			"aws": {
				"default": "default-cfg",
				"east2":   "east2-cfg",
			},
		},
	}
	require.Equal(t, "east2-cfg", e.configFor(leaf))
}

func TestConfigForWalksNestedCompositesUntilRemap(t *testing.T) {
	outer := &Node{
		Address:             "resource.outer.wrap.x",
		Kind:                NodeComposite,
		Alias:               "outer",
		ConfigurationsRemap: map[string]ConfigRef{"aws": {Alias: "aws", Configuration: "east2"}},
	}
	inner := &Node{
		Address:   "resource.outer.wrap.x/resource.inner.cluster.y",
		Kind:      NodeComposite,
		Alias:     "inner",
		Composite: outer.Address,
	}
	leaf := &Node{
		Address:   inner.Address + "/resource.aws.instance.worker",
		Alias:     "aws",
		Composite: inner.Address,
	}
	e := &Executor{
		DAG: &DAG{Nodes: map[string]*Node{
			outer.Address: outer,
			inner.Address: inner,
			leaf.Address:  leaf,
		}},
		Configurations: map[string]map[string]any{
			"aws": {
				"default": "default-cfg",
				"east2":   "east2-cfg",
			},
		},
	}
	require.Equal(t, "east2-cfg", e.configFor(leaf))
}

func TestConfigForReturnsNilWhenAliasMissing(t *testing.T) {
	leaf := &Node{
		Address:       "resource.aws.instance.web",
		Alias:         "aws",
		Configuration: "ghost",
	}
	e := &Executor{
		DAG: &DAG{Nodes: map[string]*Node{leaf.Address: leaf}},
		Configurations: map[string]map[string]any{
			"aws": {"default": "default-cfg"},
		},
	}
	require.Nil(t, e.configFor(leaf))
}

func TestConfigRefString(t *testing.T) {
	cfgs := map[string]map[string]any{
		"aws": {"default": "default-cfg", "east2": "east2-cfg"},
	}

	plain := &Node{Address: "resource.aws.instance.a", Alias: "aws"}
	ePlain := &Executor{
		DAG:            &DAG{Nodes: map[string]*Node{plain.Address: plain}},
		Configurations: cfgs,
	}
	require.Equal(t, "", ePlain.configRefString(plain),
		"a default configuration records no ref; the address determines it")

	aliased := &Node{Address: "resource.aws.instance.b", Alias: "aws", Configuration: "east2"}
	eAliased := &Executor{
		DAG:            &DAG{Nodes: map[string]*Node{aliased.Address: aliased}},
		Configurations: cfgs,
	}
	require.Equal(t, "aws.east2", eAliased.configRefString(aliased))

	composite := &Node{
		Address:             "resource.net.cluster.east",
		Kind:                NodeComposite,
		Alias:               "net",
		ConfigurationsRemap: map[string]ConfigRef{"aws": {Alias: "aws", Configuration: "east2"}},
	}
	internal := &Node{
		Address:   "resource.net.cluster.east/resource.aws.instance.worker",
		Alias:     "aws",
		Composite: composite.Address,
	}
	eRemap := &Executor{
		DAG: &DAG{Nodes: map[string]*Node{
			composite.Address: composite,
			internal.Address:  internal,
		}},
		Configurations: cfgs,
	}
	require.Equal(t, "aws.east2", eRemap.configRefString(internal))
}

func TestExecutorPropagatesSkippedOutputs(t *testing.T) {
	src := `
actions: {
  core: {
    echo: { hi: { echo: 'cached-value' } }
  }
}
outputs: {
  said: { value: action.core.echo.hi.echo }
}
`
	var runs int64
	first, second := runExecutorTwice(t, src, countingModules(&runs))
	require.Equal(t, "cached-value", first.Outputs["said"])
	require.Equal(t, "cached-value", second.Outputs["said"],
		"skipped action's outputs should still flow to downstream references")
}
