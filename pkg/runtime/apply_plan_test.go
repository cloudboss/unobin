package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/localstate"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

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

func (r *incrementalResource) Update(_ context.Context, _ any, prior any) (any, error) {
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

func incrementalModules(c *incrementalResourceCounters) map[string]*Module {
	return map[string]*Module{
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
	snap := state.NewSnapshot(state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
		store.DeploymentID())
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
	mods := incrementalModules(counters)
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Store:   store,
		Stack:   state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
	}
	_, err := planAndApply(exec)
	return err
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	mods := resourceModules(&c)
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Store:   store,
		Stack:   stack,
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	mods := resourceModules(&c)
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Inputs:  map[string]any{"configs": map[string]any{"alpha": int64(1), "beta": int64(2)}},
		Store:   store,
		Stack:   stack,
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	mods := testModules()
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Inputs: map[string]any{
			"names": map[string]any{"alpha": "hello-alpha", "beta": "hello-beta"},
		},
		Store: store,
		Stack: stack,
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	var runs int64
	mods := countingModules(&runs)
	inputs := map[string]any{
		"names": map[string]any{"alpha": "first", "beta": "second"},
	}
	apply := func() {
		applyOnce(t, &Executor{
			DAG:     BuildDAG(parseStack(t, src), mods),
			Modules: mods,
			Inputs:  inputs,
			Store:   store,
			Stack:   stack,
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	mods := testModules()
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Inputs: map[string]any{
			"keys": map[string]any{"alpha": "alpha-key", "beta": "beta-key"},
		},
		Store: store,
		Stack: stack,
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	mods := resourceModules(&c)

	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Store:   store,
		Stack:   stack,
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
	mods := resourceModules(&c)
	mods["w"] = &Module{
		Name: "w",
		Composites: map[string]*CompositeType{
			"box": {Name: "box", Body: composite, Modules: mods},
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Inputs: map[string]any{
			"configs": map[string]any{"alpha": int64(1), "beta": int64(2)},
		},
		Store: store,
		Stack: stack,
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
	require.Equal(t, state.EntryModuleCall, addrs["resource.w.box.many['alpha']"])
	require.Equal(t, state.EntryModuleCall, addrs["resource.w.box.many['beta']"])
	require.Equal(t, state.EntryLeaf, addrs["resource.w.box.many['alpha']/core.thing.only"])
	require.Equal(t, state.EntryLeaf, addrs["resource.w.box.many['beta']/core.thing.only"])
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
	mods := resourceModules(&c)
	mods["w"] = &Module{
		Name: "w",
		Composites: map[string]*CompositeType{
			"box": {Name: "box", Body: composite, Modules: mods},
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	apply := func(configs map[string]any) {
		applyOnce(t, &Executor{
			DAG:     BuildDAG(parseStack(t, src), mods),
			Modules: mods,
			Inputs:  map[string]any{"configs": configs},
			Store:   store,
			Stack:   stack,
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
	require.True(t, addrs["resource.w.box.many['alpha']/core.thing.only"])
	require.False(t, addrs["resource.w.box.many['beta']"])
	require.False(t, addrs["resource.w.box.many['beta']/core.thing.only"])
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
	mods := resourceModules(&c)
	mods["w"] = &Module{
		Name: "w",
		Composites: map[string]*CompositeType{
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Store:   store,
		Stack:   stack,
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
		[]string{"resource.w.box.x", "resource.w.box.x/core.thing.one"},
		addresses)
	require.Contains(t, types, state.EntryModuleCall)
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
  inner-mod: {
    cluster: { only: { path: var.target } }
  }
}

outputs: {
  path: { value: resource.inner-mod.cluster.only.path }
}
`)
	var c resourceCounters
	mods := resourceModules(&c)
	mods["outer-mod"] = &Module{
		Name: "outer-mod",
		Composites: map[string]*CompositeType{
			"layer": {Name: "layer", Body: layerBody},
		},
	}
	mods["inner-mod"] = &Module{
		Name: "inner-mod",
		Composites: map[string]*CompositeType{
			"cluster": {Name: "cluster", Body: clusterBody},
		},
	}
	src := `
resources: {
  outer-mod: { layer: { mine: { target: 'alpha' } } }
}
outputs: {
  out: { value: resource.outer-mod.layer.mine.path }
}
`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}

	planExec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Store:   store,
		Stack:   stack,
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
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Store:   store,
		Stack:   stack,
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
			"resource.outer-mod.layer.mine",
			"resource.outer-mod.layer.mine/inner-mod.cluster.only",
			"resource.outer-mod.layer.mine/inner-mod.cluster.only/core.thing.x",
		},
		addresses,
		"both boundaries persist as module-call records, plus the deepest leaf")
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
	mods := resourceModules(&c)
	mods["w"] = &Module{
		Name: "w",
		Composites: map[string]*CompositeType{
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}

	planAndApply := func(src string) *Plan {
		exec := &Executor{
			DAG:     BuildDAG(parseStack(t, src), mods),
			Modules: mods,
			Store:   store,
			Stack:   stack,
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
			"resource.w.box.x/core.thing.one",
			"resource.w.box.x/core.thing.two",
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
	mods := resourceModules(&c)
	mods["w"] = &Module{
		Name: "w",
		Composites: map[string]*CompositeType{
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}

	planExec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Inputs:  map[string]any{"who": "world"},
		Store:   store,
		Stack:   stack,
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
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Store:   store,
		Stack:   stack,
	}
	_, err = applyExec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, int64(1), c.creates)

	snap, err := store.Current()
	require.NoError(t, err)
	leaf := findEntryByAddr(snap, "resource.w.hello.x/core.thing.greet")
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
	mods := resourceModules(&c)
	mods["w"] = &Module{
		Name: "w",
		Composites: map[string]*CompositeType{
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}

	planAndApply := func(src string) {
		exec := &Executor{
			DAG:     BuildDAG(parseStack(t, src), mods),
			Modules: mods,
			Store:   store,
			Stack:   stack,
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	mods := resourceModules(&c)

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	// Drift: a separate apply changes state out from under our plan.
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	mods := resourceModules(&c)
	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}

	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), nil),
		Modules: map[string]*Module{},
		Store:   store,
		Stack:   stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	// Apply against a different stack identity.
	exec.Stack = state.StackInfo{Name: "different", Version: "v0", Commit: "c0"}
	_, err = exec.ApplyPlan(context.Background(), pf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "different")
}

func TestEncodeDecodePlan(t *testing.T) {
	plan := &Plan{
		Stack:        state.StackInfo{Name: "x", Version: "v1", Commit: "abc"},
		DeploymentID: "prod",
		StateRev:     "2026-05-01T00:00:00.000000000Z",
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
	require.Equal(t, plan.Stack.Name, pf.Stack.Name)
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
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	var resCounters resourceCounters
	var actionRuns int64
	mods := map[string]*Module{
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
			DAG:     BuildDAG(parseStack(t, stackSrc), mods),
			Modules: mods,
			Store:   store,
			Stack:   stack,
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
