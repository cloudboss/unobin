package runtime

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/cloudboss/unobin/pkg/ubtest"
	"github.com/stretchr/testify/require"
)

func executorFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/executor", name)
}

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
				"echo": MakeAction[echoAction, any, any](),
				"fail": MakeAction[failingAction, any, any](),
			},
			DataSources: map[string]DataSourceRegistration{
				"lookup": MakeDataSource[lookupDataSource, any, any](),
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

func executorTestExecutor(
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

func runExecutor(t *testing.T, src string, inputs map[string]any) (*ExecResult, error) {
	t.Helper()
	libs := testModules()
	exec := executorTestExecutor(t, src, libs, newStateStore(t),
		state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"})
	exec.Inputs = inputs
	return planAndApply(exec)
}

func TestExecutorRequiresStore(t *testing.T) {
	exec := executorTestExecutor(t, `description: 'x'`, testModules(), nil, state.FactoryInfo{})
	_, err := exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Store")
}

func TestExecutorOutputOnly(t *testing.T) {
	res, err := runExecutor(t,
		executorFixture(t, "executor-output-only"),
		map[string]any{"region": "us-east-1"})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"region": "us-east-1"}, res.Outputs)
}

func TestExecutorActionRuns(t *testing.T) {
	res, err := runExecutor(t, executorFixture(t, "executor-action-runs"), nil)
	require.NoError(t, err)
	require.Equal(t, "hello", res.Outputs["said"])
	require.Equal(t, int64(5), res.Outputs["letters"])
}

func TestExecutorInputFlowsToAction(t *testing.T) {
	res, err := runExecutor(t,
		executorFixture(t, "executor-input-flows-to-action"),
		map[string]any{"name": "world"})
	require.NoError(t, err)
	require.Equal(t, "world", res.Outputs["said"])
}

func TestExecutorDataSource(t *testing.T) {
	res, err := runExecutor(t,
		executorFixture(t, "executor-data-source"),
		map[string]any{"key": "abc"})
	require.NoError(t, err)
	require.Equal(t, "looked-up:abc", res.Outputs["found"])
}

func TestExecutorActionDependsOnAction(t *testing.T) {
	res, err := runExecutor(t, executorFixture(t, "executor-action-depends-on-action"), nil)
	require.NoError(t, err)
	require.Equal(t, "one", res.Outputs["result"])
}

func TestExecutorPropagatesActionError(t *testing.T) {
	_, err := runExecutor(t, executorFixture(t, "executor-propagates-action-error"), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "action.f")
	require.Contains(t, err.Error(), "intentional failure")
}

type resourceCounters struct {
	creates int64
	updates int64
	deletes int64
	// readFn lets a test control what countingResource.Read returns;
	// nil means Read returns prior unchanged (no drift, not gone).
	readFn func(prior any) (any, error)
	// gotUpdatePrior captures the Prior the last Update received, so a
	// test can assert what reached the resource through plan and apply.
	gotUpdatePrior *Prior[countingResource, any]
	// gotInputMigratePrior captures the Prior an inputMigratingResource's
	// Update received, so a test can assert the migrated inputs reached it.
	gotInputMigratePrior *Prior[inputMigratingResource, any]
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

func (r *countingResource) Update(
	_ context.Context, _ any, prior Prior[countingResource, any],
) (any, error) {
	atomic.AddInt64(&r.counters.updates, 1)
	r.counters.gotUpdatePrior = &prior
	m, _ := prior.Outputs.(map[string]any)
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

func (r *countingResourceV2) Update(
	ctx context.Context, cfg any, prior Prior[countingResourceV2, any],
) (any, error) {
	return r.countingResource.Update(ctx, cfg, Prior[countingResource, any]{Outputs: prior.Outputs})
}

// migratingCountingResource is countingResourceV2 with a Migrate
// method that rewrites `id` to `name-id` in state, used by the plan
// test for the migration happy path.
type migratingCountingResource struct {
	countingResource `ub:",squash"`
}

func (r *migratingCountingResource) SchemaVersion() int { return 2 }

func (r *migratingCountingResource) Update(
	ctx context.Context, cfg any, prior Prior[migratingCountingResource, any],
) (any, error) {
	return r.countingResource.Update(ctx, cfg, Prior[countingResource, any]{Outputs: prior.Outputs})
}

func (r *migratingCountingResource) Migrate(_ int, prior MigrationState) (MigrationState, error) {
	return MigrationState{
		Inputs:  prior.Inputs,
		Outputs: renamedKey(prior.Outputs, "id", "name-id"),
	}, nil
}

// inputMigratingResource bumps SchemaVersion to 2 and migrates both
// halves of a prior entry: the input field `label` becomes `name` and
// the output `id` becomes `name-id`. Its Update records the Prior it
// receives so a test can assert the migrated inputs reached the resource
// through plan and apply.
type inputMigratingResource struct {
	countingResource `ub:",squash"`
}

func (r *inputMigratingResource) SchemaVersion() int { return 2 }

func (r *inputMigratingResource) Update(
	_ context.Context, _ any, prior Prior[inputMigratingResource, any],
) (any, error) {
	atomic.AddInt64(&r.counters.updates, 1)
	r.counters.gotInputMigratePrior = &prior
	m, _ := prior.Outputs.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	m["name"] = r.Name
	m["size"] = r.Size
	return m, nil
}

func (r *inputMigratingResource) Migrate(_ int, prior MigrationState) (MigrationState, error) {
	return MigrationState{
		Inputs:  renamedKey(prior.Inputs, "label", "name"),
		Outputs: renamedKey(prior.Outputs, "id", "name-id"),
	}, nil
}

// renamedKey returns a copy of m with the value at from moved to to,
// modeling a renamed field across a schema bump.
func renamedKey(m map[string]any, from, to string) map[string]any {
	out := map[string]any{}
	maps.Copy(out, m)
	if v, ok := out[from]; ok {
		out[to] = v
		delete(out, from)
	}
	return out
}

func resourceModules(c *resourceCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[countingResource, any, any](
					func() *countingResource { return &countingResource{counters: c} },
				),
			},
			Functions: map[string]FunctionType{
				"all": {Name: "all", ArgCount: 1, Func: fnAllBools},
			},
		},
	}
}

// inputMigratingLibs registers core.thing as an inputMigratingResource so
// schema-bump tests can drive plan, apply, and refresh against a resource
// whose Migrate upgrades both halves of a prior entry.
func inputMigratingLibs(c *resourceCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[inputMigratingResource, any, any](
					func() *inputMigratingResource {
						return &inputMigratingResource{
							countingResource: countingResource{counters: c},
						}
					},
				),
			},
		},
	}
}

// defaultingLibs registers core.thing (a countingResource) with a Value
// default of 7 for `size`, so tests can exercise the plan-time overlay of
// declared defaults onto prior inputs.
func defaultingLibs(c *resourceCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[countingResource, any, any](
					func() *countingResource { return &countingResource{counters: c} },
				),
			},
			Defaults: map[string][]lang.DefaultSpec{
				"resource.thing": {{Field: "input.size", Value: "7"}},
			},
		},
	}
}

// fnAllBools mirrors core's all function so tests can call a function
// from a constraint predicate without importing the real library.
func fnAllBools(args []any) (any, error) {
	lst, ok := args[0].([]any)
	if !ok {
		return nil, fmt.Errorf("all: expected a list, got %T", args[0])
	}
	for _, el := range lst {
		b, ok := el.(bool)
		if !ok {
			return nil, fmt.Errorf("all: expected booleans, got %T", el)
		}
		if !b {
			return false, nil
		}
	}
	return true, nil
}

func TestExecutorRunsComposite(t *testing.T) {
	composite := syntaxResourceComposite(t, "box", executorFixture(t, "executor-runs-composite-1"))
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": composite,
		},
	}
	src := executorFixture(t, "executor-runs-composite-2")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := executorTestExecutor(t, src, libs, store, stack)
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
	require.Equal(t, "resource.x/resource.one", leaf.Address)
	require.Equal(t, "resource", leaf.Kind)
	require.Equal(t, &state.Selector{Alias: "core", Export: "thing"}, leaf.Selector)

	require.NotNil(t, libCall)
	require.Equal(t, "resource.x", libCall.Address)
	require.Equal(t, "resource", libCall.Kind)
	require.Equal(t, &state.Selector{Alias: "w", Export: "box"}, libCall.Selector)
	require.Equal(t, "alpha", libCall.Inputs["name"])
	require.Equal(t, "fake-alpha", libCall.Outputs["id"])
}

func TestExecutorAppliesDataComposite(t *testing.T) {
	composite := syntaxComposite(t, "box", NodeData,
		executorFixture(t, "executor-applies-data-composite-1"))
	libs := testModules()
	libs["w"] = &Library{
		Name: "w",
		DataComposites: map[string]*CompositeType{
			"box": composite,
		},
	}
	src := executorFixture(t, "executor-applies-data-composite-2")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := executorTestExecutor(t, src, libs, store, stack)
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
	require.Equal(t, "data.x", libCall.Address,
		"the boundary address has the data kind root")
	require.Equal(t, "data", libCall.Kind)
	require.Equal(t, &state.Selector{Alias: "w", Export: "box"}, libCall.Selector)
	require.Equal(t, "looked-up:abc", libCall.Outputs["value"])

	// A second plan and apply against the prior state still resolves the
	// boundary's output, so the re-plan path handles a data boundary too.
	res2 := applyOnce(t, exec)
	require.Equal(t, "looked-up:abc", res2.Outputs["out"])
}

func TestExecutorAppliesActionComposite(t *testing.T) {
	composite := syntaxComposite(t, "greet", NodeAction,
		executorFixture(t, "executor-applies-action-composite-1"))
	libs := testModules()
	libs["ops"] = &Library{
		Name: "ops",
		ActionComposites: map[string]*CompositeType{
			"greet": composite,
		},
	}
	src := executorFixture(t, "executor-applies-action-composite-2")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := executorTestExecutor(t, src, libs, store, stack)
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
	require.Equal(t, "action.hello", libCall.Address,
		"the boundary address has the action kind root")
	require.Equal(t, "hi", libCall.Outputs["said"])
}

func TestExecutorForEachResourceCreatesPerInstance(t *testing.T) {
	src := executorFixture(t, "executor-for-each-resource-creates-per-instance")
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)
	exec := executorTestExecutor(t, src, libs, store, stack)
	exec.Inputs = map[string]any{
		"configs": map[string]any{
			"alpha": int64(1),
			"beta":  int64(2),
		},
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
	require.True(t, addrs["resource.many['alpha']"], "alpha instance in state")
	require.True(t, addrs["resource.many['beta']"], "beta instance in state")
}

func TestExecutorForEachOrphanInstanceDeleted(t *testing.T) {
	src := executorFixture(t, "executor-for-each-orphan-instance-deleted")
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)
	runOnce := func(configs map[string]any) {
		exec := executorTestExecutor(t, src, libs, store, stack)
		exec.Inputs = map[string]any{"configs": configs}
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
	require.True(t, addrs["resource.many['alpha']"])
	require.False(t, addrs["resource.many['beta']"], "beta dropped from state")
}

func TestExecutorForEachRejectsList(t *testing.T) {
	src := executorFixture(t, "executor-for-each-rejects-list")
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)
	exec := executorTestExecutor(t, src, libs, store, stack)
	exec.Inputs = map[string]any{"items": []any{"a", "b"}}
	_, err := planAndApply(exec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "@for-each")
}

func TestExecutorModuleFunctionInOutput(t *testing.T) {
	res, err := runExecutor(t,
		executorFixture(t, "executor-module-function-in-output"),
		map[string]any{"name": "hello"})
	require.NoError(t, err)
	require.Equal(t, "HELLO", res.Outputs["shout"])
}

func TestExecutorModuleFunctionInsideComposite(t *testing.T) {
	layerBody := syntaxResourceComposite(t, "layer",
		executorFixture(t, "executor-module-function-inside-composite-1"))
	layerBody.Libraries = testModules()
	rootMods := map[string]*Library{
		"wrapper": {
			Name: "wrapper",
			ResourceComposites: map[string]*CompositeType{
				"layer": layerBody,
			},
		},
	}
	src := executorFixture(t, "executor-module-function-inside-composite-2")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := executorTestExecutor(t, src, rootMods, store, stack)
	res := applyOnce(t, exec)
	require.Equal(t, "HI", res.Outputs["out"])
}

func TestExecutorCompositeUsesItsOwnModules(t *testing.T) {
	// The composite declares which libraries its body uses via its own
	// imports. The runtime should resolve composite-internal lookups
	// against the composite's Libraries table, not the stack root's. This
	// is the encapsulation that lets a composite be reusable without the
	// caller needing to import everything the composite uses transitively.
	composite := syntaxResourceComposite(t, "layer",
		executorFixture(t, "executor-composite-uses-its-own-modules-1"))
	var c resourceCounters
	// "core" is registered only in the composite's Libraries, never in
	// the stack-root libs.
	composite.Libraries = resourceModules(&c)
	rootMods := map[string]*Library{
		"outer-lib": {
			Name: "outer-lib",
			ResourceComposites: map[string]*CompositeType{
				"layer": composite,
			},
		},
	}
	src := executorFixture(t, "executor-composite-uses-its-own-modules-2")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := executorTestExecutor(t, src, rootMods, store, stack)
	res, err := planAndApply(exec)
	require.NoError(t, err,
		"composite-internal core.thing should resolve via the composite's own Libraries")
	require.Equal(t, "fake-alpha", res.Outputs["out"])
	require.Equal(t, int64(1), c.creates)
}

func TestExecutorRunsNestedComposite(t *testing.T) {
	clusterBody := syntaxResourceComposite(t, "cluster",
		executorFixture(t, "executor-runs-nested-composite-1"))
	layerBody := syntaxResourceComposite(t, "layer",
		executorFixture(t, "executor-runs-nested-composite-2"))
	var c resourceCounters
	libs := resourceModules(&c)
	libs["outer-lib"] = &Library{
		Name: "outer-lib",
		ResourceComposites: map[string]*CompositeType{
			"layer": layerBody,
		},
	}
	libs["inner-lib"] = &Library{
		Name: "inner-lib",
		ResourceComposites: map[string]*CompositeType{
			"cluster": clusterBody,
		},
	}
	src := executorFixture(t, "executor-runs-nested-composite-3")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := executorTestExecutor(t, src, libs, store, stack)
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

	leafAddr := "resource.mine/resource.only/resource.x"
	leaf := byAddr[leafAddr]
	require.NotNil(t, leaf, "deepest leaf persists at fully chained address")
	require.Equal(t, state.EntryLeaf, leaf.Type)

	innerAddr := "resource.mine/resource.only"
	inner := byAddr[innerAddr]
	require.NotNil(t, inner)
	require.Equal(t, state.EntryLibraryCall, inner.Type)
	require.Equal(t, "resource", inner.Kind)
	require.Equal(t, &state.Selector{Alias: "inner-lib", Export: "cluster"}, inner.Selector)

	outerAddr := "resource.mine"
	outer := byAddr[outerAddr]
	require.NotNil(t, outer)
	require.Equal(t, state.EntryLibraryCall, outer.Type)
	require.Equal(t, "resource", outer.Kind)
	require.Equal(t, &state.Selector{Alias: "outer-lib", Export: "layer"}, outer.Selector)
}

func TestExecutorNestedCompositeEncapsulation(t *testing.T) {
	// Inner's leaf produces {id, name, size}; inner only publishes
	// {path}. Outer's outputs reference the boundary's published
	// outputs, not the leaf's internals.
	clusterBody := syntaxResourceComposite(t, "cluster",
		executorFixture(t, "executor-nested-composite-encapsulation-1"))
	layerBody := syntaxResourceComposite(t, "layer",
		executorFixture(t, "executor-nested-composite-encapsulation-2"))
	var c resourceCounters
	libs := resourceModules(&c)
	libs["outer-lib"] = &Library{
		Name: "outer-lib",
		ResourceComposites: map[string]*CompositeType{
			"layer": layerBody,
		},
	}
	libs["inner-lib"] = &Library{
		Name: "inner-lib",
		ResourceComposites: map[string]*CompositeType{
			"cluster": clusterBody,
		},
	}

	t.Run("only published outputs cross the boundary", func(t *testing.T) {
		src := executorFixture(t, "executor-nested-composite-encapsulation-3")
		store := newStateStore(t)
		stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
		exec := executorTestExecutor(t, src, libs, store, stack)
		res := applyOnce(t, exec)
		require.Equal(t, "beta", res.Outputs["out"])

		snap, err := store.Current()
		require.NoError(t, err)
		var inner *state.Entry
		for _, e := range snap.Entries {
			if e.Address == "resource.mine/resource.only" {
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
		// Outer attempts to reference resource.mine.size, which is the
		// inner leaf's `size` field, not in inner's `outputs:` block. The
		// reference must fail at eval time because outer scope holds only the
		// boundary's published map.
		src := executorFixture(t, "executor-nested-composite-encapsulation-4")
		store := newStateStore(t)
		stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
		exec := executorTestExecutor(t, src, libs, store, stack)
		_, err := planAndApply(exec)
		require.Error(t, err,
			"outer scope must not expose the inner leaf's internal fields")
		require.Contains(t, err.Error(), "not found")
	})
}

func TestExecutorCompositeInternalDataAndAction(t *testing.T) {
	composite := syntaxResourceComposite(t, "box",
		executorFixture(t, "executor-composite-internal-data-and-action-1"))
	libs := testModules()
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": composite,
		},
	}
	src := executorFixture(t, "executor-composite-internal-data-and-action-2")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := executorTestExecutor(t, src, libs, store, stack)
	res := applyOnce(t, exec)
	require.Equal(t, "looked-up:banana", res.Outputs["result"])

	snap, err := store.Current()
	require.NoError(t, err)
	var actionEntry, libCall *state.Entry
	for _, e := range snap.Entries {
		switch e.Address {
		case "resource.x/action.say":
			actionEntry = e
		case "resource.x":
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
	src := executorFixture(t, "executor-creates-resource")
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)
	exec := executorTestExecutor(t, src, libs, store, stack)
	res := applyOnce(t, exec)
	require.Equal(t, "fake-alpha", res.Outputs["id"])
	require.Equal(t, int64(1), atomic.LoadInt64(&c.creates))
	require.Equal(t, int64(0), atomic.LoadInt64(&c.updates))
}

func TestExecutorSameInputsNoCreateOrUpdate(t *testing.T) {
	src := executorFixture(t, "executor-same-inputs-no-create-or-update")
	var c resourceCounters
	runExecutorTwice(t, src, resourceModules(&c))
	require.Equal(t, int64(1), atomic.LoadInt64(&c.creates))
	require.Equal(t, int64(0), atomic.LoadInt64(&c.updates))
}

func TestExecutorChangedInputsTriggersUpdate(t *testing.T) {
	first := executorFixture(t, "executor-changed-inputs-triggers-update-1")
	second := executorFixture(t, "executor-changed-inputs-triggers-update-2")
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)

	applyOnce(t, executorTestExecutor(t, first, libs, store, stack))
	applyOnce(t, executorTestExecutor(t, second, libs, store, stack))

	require.Equal(t, int64(1), atomic.LoadInt64(&c.creates))
	require.Equal(t, int64(1), atomic.LoadInt64(&c.updates))
}

func TestExecutorReplaceFieldChangeTriggersDeleteAndCreate(t *testing.T) {
	first := executorFixture(t, "executor-replace-field-change-triggers-delete-and-create-1")
	second := executorFixture(t, "executor-replace-field-change-triggers-delete-and-create-2")
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)

	applyOnce(t, executorTestExecutor(t, first, libs, store, stack))
	applyOnce(t, executorTestExecutor(t, second, libs, store, stack))

	require.Equal(t, int64(2), atomic.LoadInt64(&c.creates),
		"replace destroys the old and creates a new")
	require.Equal(t, int64(1), atomic.LoadInt64(&c.deletes),
		"replace deletes the old before creating")
	require.Equal(t, int64(0), atomic.LoadInt64(&c.updates),
		"replace bypasses Update")
}

func TestExecutorOrphanResourceDeleted(t *testing.T) {
	first := executorFixture(t, "executor-orphan-resource-deleted-1")
	second := executorFixture(t, "executor-orphan-resource-deleted-2")
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)

	applyOnce(t, executorTestExecutor(t, first, libs, store, stack))
	applyOnce(t, executorTestExecutor(t, second, libs, store, stack))

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
	require.Equal(t, []string{"resource.keep"}, addresses)
}

func TestExecutorResourceMissingType(t *testing.T) {
	_, err := runExecutor(t, executorFixture(t, "executor-resource-missing-type"), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not-a-thing")
}

func TestExecutorUnknownModule(t *testing.T) {
	_, err := runExecutor(t, executorFixture(t, "executor-unknown-module"), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")
}

func TestExecutorUnknownActionType(t *testing.T) {
	_, err := runExecutor(t, executorFixture(t, "executor-unknown-action-type"), nil)
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

func newStateStore(t *testing.T) *local.Store {
	t.Helper()
	s, err := local.NewStore(t.TempDir(), "test-stack", "prod", encrypters.Noop{})
	require.NoError(t, err)
	return s
}

func runExecutorTwice(
	t *testing.T, src string, libraries map[string]*Library,
) (*ExecResult, *ExecResult) {
	t.Helper()
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t, src, libraries)

	first := applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libraries, Store: store, Factory: stack,
	})
	second := applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libraries, Store: store, Factory: stack,
	})
	return first, second
}

func countingModules(runs *int64) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Actions: map[string]ActionRegistration{
				"echo": MakeActionWith[countingAction, any, any](
					func() *countingAction { return &countingAction{runs: runs} },
				),
			},
		},
	}
}

func TestExecutorPersistsSnapshot(t *testing.T) {
	store := newStateStore(t)
	libs := testModules()
	exec := executorTestExecutor(t,
		executorFixture(t, "executor-persists-snapshot"),
		libs,
		store,
		state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"})
	res := applyOnce(t, exec)
	require.NotEmpty(t, res.WrittenRev)

	gotRev, err := store.CurrentRev()
	require.NoError(t, err)
	require.Equal(t, res.WrittenRev, gotRev)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 1)
	require.Equal(t, "action.hi", snap.Entries[0].Address)
	require.Equal(t, state.EntryAction, snap.Entries[0].Type)
	require.NotEmpty(t, snap.Entries[0].TriggerHash)
}

func TestExecutorSkipsActionWhenInputsUnchanged(t *testing.T) {
	src := executorFixture(t, "executor-skips-action-when-inputs-unchanged")
	var runs int64
	runExecutorTwice(t, src, countingModules(&runs))
	require.Equal(t, int64(1), atomic.LoadInt64(&runs),
		"action should run once across two executions when inputs are unchanged")
}

func TestExecutorAlwaysTriggerReruns(t *testing.T) {
	src := executorFixture(t, "executor-always-trigger-reruns")
	var runs int64
	runExecutorTwice(t, src, countingModules(&runs))
	require.Equal(t, int64(2), atomic.LoadInt64(&runs),
		"action with @trigger: 'always' should run on every execution")
}

func TestExecutorExplicitTriggerSkipsWhenSame(t *testing.T) {
	src := executorFixture(t, "executor-explicit-trigger-skips-when-same")
	var runs int64
	runExecutorTwice(t, src, countingModules(&runs))
	require.Equal(t, int64(1), atomic.LoadInt64(&runs))
}

func TestConfigForUsesLibraryConfigNode(t *testing.T) {
	leaf := &Node{Address: "resource.web", Alias: "aws"}
	configNode := &Node{Address: "library-config.aws", Kind: NodeLibraryConfig, Alias: "aws"}
	e := &Executor{DAG: &DAG{Nodes: map[string]*Node{
		leaf.Address:       leaf,
		configNode.Address: configNode,
	}}}
	e.storeInternalConfiguration(configNode.Address, "decoded-cfg")
	require.Equal(t, "decoded-cfg", e.configFor(leaf))
}

func TestConfigForEmptyConfigSynthesizesValue(t *testing.T) {
	leaf := &Node{Address: "resource.web", Alias: "aws"}
	e := &Executor{
		DAG: &DAG{Nodes: map[string]*Node{leaf.Address: leaf}},
		Libraries: map[string]*Library{
			"aws": {
				Configuration: &cfg.ConfigurationType[*struct{}]{
					New: func() *struct{} { return &struct{}{} },
				},
			},
		},
	}
	require.IsType(t, &struct{}{}, e.configFor(leaf))
}

func TestConfigForNoConfigReturnsNil(t *testing.T) {
	leaf := &Node{Address: "resource.web", Alias: "aws"}
	e := &Executor{
		DAG:       &DAG{Nodes: map[string]*Node{leaf.Address: leaf}},
		Libraries: map[string]*Library{"aws": {}},
	}
	require.Nil(t, e.configFor(leaf))
}

func TestExecutorPropagatesSkippedOutputs(t *testing.T) {
	src := executorFixture(t, "executor-propagates-skipped-outputs")
	var runs int64
	first, second := runExecutorTwice(t, src, countingModules(&runs))
	require.Equal(t, "cached-value", first.Outputs["said"])
	require.Equal(t, "cached-value", second.Outputs["said"],
		"skipped action's outputs should still flow to downstream references")
}

func TestMergeAttrs(t *testing.T) {
	tests := []struct {
		name    string
		inputs  map[string]any
		outputs map[string]any
		want    map[string]any
	}{
		{
			name:    "input passes through when no same-named output",
			inputs:  map[string]any{"path": "/tmp/f"},
			outputs: map[string]any{"sha256": "abc"},
			want:    map[string]any{"path": "/tmp/f", "sha256": "abc"},
		},
		{
			name:    "output wins on a name collision",
			inputs:  map[string]any{"name": "Foo"},
			outputs: map[string]any{"name": "foo"},
			want:    map[string]any{"name": "foo"},
		},
		{
			name:    "output-only field is present",
			inputs:  map[string]any{},
			outputs: map[string]any{"id": "x"},
			want:    map[string]any{"id": "x"},
		},
		{
			name:    "nil outputs leaves inputs readable",
			inputs:  map[string]any{"path": "/tmp/f"},
			outputs: nil,
			want:    map[string]any{"path": "/tmp/f"},
		},
		{
			name:    "nil inputs leaves outputs readable",
			inputs:  nil,
			outputs: map[string]any{"id": "x"},
			want:    map[string]any{"id": "x"},
		},
		{
			name:    "both nil yields an empty map",
			inputs:  nil,
			outputs: nil,
			want:    map[string]any{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, mergeAttrs(tt.inputs, tt.outputs))
		})
	}
}
