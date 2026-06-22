package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/cloudboss/unobin/pkg/ubtest"
	"github.com/stretchr/testify/require"
)

func applyPlanTestExecutor(
	t *testing.T,
	src string,
	libs map[string]*Library,
	store state.Backend,
	factory state.FactoryInfo,
) *Executor {
	t.Helper()
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	return &Executor{
		DAG: dag, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: factory,
	}
}

func applyPlanFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/apply-plan", name)
}

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
				"thing": MakeResourceWith[orderResource, any, any](
					func() *orderResource { return &orderResource{rec: rec} },
				),
			},
		},
	}
}

func selectorChangeModules(oldC, newC *resourceCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"old": MakeResourceWith[countingResource, any, any](
					func() *countingResource { return &countingResource{counters: oldC} },
				),
				"new": MakeResourceWith[countingResource, any, any](
					func() *countingResource { return &countingResource{counters: newC} },
				),
			},
		},
	}
}

type countedAction struct {
	Echo string
	runs *int64
}

func (a *countedAction) Run(_ context.Context, _ any) (any, error) {
	atomic.AddInt64(a.runs, 1)
	return map[string]any{"echo": a.Echo}, nil
}

func actionSelectorChangeModules(oldRuns, newRuns *int64) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Actions: map[string]ActionRegistration{
				"old": MakeActionWith[countedAction, any, any](
					func() *countedAction { return &countedAction{runs: oldRuns} },
				),
				"new": MakeActionWith[countedAction, any, any](
					func() *countedAction { return &countedAction{runs: newRuns} },
				),
			},
		},
	}
}

func TestResourceSelectorChangeReplacesResource(t *testing.T) {
	oldC := &resourceCounters{}
	newC := &resourceCounters{}
	libs := selectorChangeModules(oldC, newC)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	oldSrc := applyPlanFixture(t, "resource-selector-change-replaces-resource-1")
	newSrc := applyPlanFixture(t, "resource-selector-change-replaces-resource-2")
	applyOnce(t, applyPlanTestExecutor(t, oldSrc, libs, store, stack))

	exec := applyPlanTestExecutor(t, newSrc, libs, store, stack)
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	step := findStep(t, plan, "resource.one")
	require.Equal(t, DecisionReplace, step.Decision)
	require.Equal(t, &state.Selector{Alias: "core", Export: "old"}, step.PriorSelector)
	require.Equal(t, &state.Selector{Alias: "core", Export: "new"}, step.Selector)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)
	_, err = exec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.EqualValues(t, 1, oldC.deletes)
	require.EqualValues(t, 1, newC.creates)
	require.EqualValues(t, 0, oldC.updates)

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.one")
	require.NotNil(t, ent)
	require.Equal(t, &state.Selector{Alias: "core", Export: "new"}, ent.Selector)
}

func TestActionSelectorChangeRerunsAction(t *testing.T) {
	var oldRuns int64
	var newRuns int64
	libs := actionSelectorChangeModules(&oldRuns, &newRuns)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	oldSrc := applyPlanFixture(t, "action-selector-change-reruns-action-1")
	newSrc := applyPlanFixture(t, "action-selector-change-reruns-action-2")
	applyOnce(t, applyPlanTestExecutor(t, oldSrc, libs, store, stack))

	exec := applyPlanTestExecutor(t, newSrc, libs, store, stack)
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	step := findStep(t, plan, "action.one")
	require.Equal(t, DecisionRerun, step.Decision)
	applyOnce(t, exec)
	require.EqualValues(t, 1, oldRuns)
	require.EqualValues(t, 1, newRuns)
}

func TestDestroyDeletesDependentsFirst(t *testing.T) {
	rec := &deleteOrder{}
	libs := orderModules(rec)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	withBoth := applyPlanFixture(t, "destroy-deletes-dependents-first")
	exec := applyPlanTestExecutor(t, withBoth, libs, store, stack)
	_, err := planAndApply(exec)
	require.NoError(t, err)

	// Remove both from source so the next apply destroys them. b
	// depended on a, so b must be deleted before a.
	empty := applyPlanTestExecutor(t, `description: 'gone'`, libs, store, stack)
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
			Name: "aws",
			Configuration: &cfg.ConfigurationType[any]{
				New: func() any { return &endpointConfiguration{} },
			},
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[cfgResource, any, any](
					func() *cfgResource { return &cfgResource{capture: capture} },
				),
			},
		},
	}
}

func TestDestroyUsesCurrentAliasConfig(t *testing.T) {
	capture := &cfgCapture{}
	libs := cfgCapturingModules(capture)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	withResource := applyPlanFixture(t, "destroy-uses-current-alias-config")
	exec := applyPlanTestExecutor(t, withResource, libs, store, stack)
	_, err := planAndApply(exec)
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 1)

	destroySource := `
library-configs: { aws: { endpoint: 'https://delete.example' } }
`
	empty := applyPlanTestExecutor(t, destroySource, libs, store, stack)
	_, err = planAndApply(empty)
	require.NoError(t, err)
	require.True(t, capture.deleted)
	require.Equal(t, "https://delete.example", endpointOf(capture.deleteCfg))

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
				"inc": MakeResourceWith[incrementalResource, any, any](
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
		Kind:          "resource",
		Selector:      &state.Selector{Alias: "core", Export: "inc"},
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

func seedIncrementalState(t *testing.T, store *local.Store, entries ...*state.Entry) {
	t.Helper()
	factory := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	snap := state.NewSnapshot(factory, store.Stack())
	snap.Entries = entries
	rev, err := store.Write(snap)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))
}

func applyIncrementalPlan(
	t *testing.T,
	store *local.Store,
	counters *incrementalResourceCounters,
	src string,
) error {
	t.Helper()
	libs := incrementalModules(counters)
	exec := applyPlanTestExecutor(t, src, libs, store,
		state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"})
	_, err := planAndApply(exec)
	return err
}

func TestPlanDestroyTearsDownEverything(t *testing.T) {
	rec := &deleteOrder{}
	libs := orderModules(rec)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	src := applyPlanFixture(t, "plan-destroy-tears-down-everything")
	build := func(destroy bool) *Executor {
		exec := applyPlanTestExecutor(t, src, libs, store, stack)
		exec.Destroy = destroy
		return exec
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
	t *testing.T, store *local.Store, libs map[string]*Library,
	src string, inputs map[string]any,
) *ExecResult {
	t.Helper()
	exec := applyPlanTestExecutor(t, src, libs, store,
		state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"})
	exec.Inputs = inputs
	res, err := planAndApply(exec)
	require.NoError(t, err)
	return res
}

func destroyStack(
	t *testing.T, store *local.Store, libs map[string]*Library, src string,
) (*ExecResult, error) {
	t.Helper()
	exec := applyPlanTestExecutor(t, src, libs, store,
		state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"})
	exec.Destroy = true
	return planAndApply(exec)
}

func requireEmptyState(t *testing.T, store *local.Store) {
	t.Helper()
	snap, err := store.Current()
	require.NoError(t, err)
	require.Empty(t, snap.Entries)
}

func TestPlanDestroyMarksEveryEntryForDeletion(t *testing.T) {
	rec := &deleteOrder{}
	libs := orderModules(rec)
	store := newStateStore(t)
	src := applyPlanFixture(t, "plan-destroy-marks-every-entry-for-deletion")
	applyStack(t, store, libs, src, nil)

	exec := applyPlanTestExecutor(t, src, libs, store,
		state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"})
	exec.Destroy = true
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
	src := applyPlanFixture(t, "plan-destroy-with-no-prior-state-is-empty")

	res, err := destroyStack(t, store, libs, src)
	require.NoError(t, err)
	require.Empty(t, rec.order)
	require.Empty(t, res.Outputs)
	requireEmptyState(t, store)
}

func TestDestroyChainDeletesInReverseOrder(t *testing.T) {
	src := applyPlanFixture(t, "destroy-chain-deletes-in-reverse-order")
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
	src := applyPlanFixture(t, "destroy-for-each-deletes-all-instances")
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
	composite := syntaxResourceComposite(t, "box", applyPlanFixture(t, "destroy-composite-1"))
	libs["w"] = &Library{
		Name:               "w",
		ResourceComposites: map[string]*CompositeType{"box": composite},
	}
	store := newStateStore(t)
	src := applyPlanFixture(t, "destroy-composite-2")
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
				"thing": MakeResourceWith[orderResource, any, any](
					func() *orderResource { return &orderResource{rec: rec} },
				),
			},
			Actions: map[string]ActionRegistration{
				"echo": MakeActionWith[countingAction, any, any](
					func() *countingAction { return &countingAction{runs: &runs} },
				),
			},
		},
	}
	store := newStateStore(t)
	src := applyPlanFixture(t, "destroy-removes-action-without-running-it")
	applyStack(t, store, libs, src, nil)
	require.Equal(t, int64(1), atomic.LoadInt64(&runs))

	_, err := destroyStack(t, store, libs, src)
	require.NoError(t, err)
	require.Equal(t, int64(1), atomic.LoadInt64(&runs),
		"an action has no delete step and must not run during destroy")
	require.Equal(t, []string{"r"}, rec.order)
	requireEmptyState(t, store)
}

func TestDestroyClearsActionAndLibraryCallRecords(t *testing.T) {
	compositeBody := syntaxResourceComposite(t, "box",
		applyPlanFixture(t, "destroy-clears-action-and-library-call-records-1"))
	libs := testModules()
	libs["w"] = &Library{
		Name:               "w",
		ResourceComposites: map[string]*CompositeType{"box": compositeBody},
	}
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	// No leaf resources: only a root action, a library-call record, and
	// the composite's internal action. This is the shape that used to
	// plan as "No changes".
	src := applyPlanFixture(t, "destroy-clears-action-and-library-call-records-2")
	exec := applyPlanTestExecutor(t, src, libs, store, stack)
	_, err := planAndApply(exec)
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	require.NotEmpty(t, snap.Entries)

	destroyer := applyPlanTestExecutor(t, src, libs, store, stack)
	destroyer.Destroy = true
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
	src := applyPlanFixture(t, "destroy-skips-delete-for-already-gone-resource")

	create := applyPlanTestExecutor(t, src, libs, store, stack)
	_, err := planAndApply(create)
	require.NoError(t, err)

	// The resource vanishes out of band; its read now reports it gone.
	c.readFn = func(any) (any, error) { return nil, ErrNotFound }

	destroyer := applyPlanTestExecutor(t, src, libs, store, stack)
	destroyer.Destroy = true
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
	src := applyPlanFixture(t, "apply-persists-depends-on")
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)
	exec := applyPlanTestExecutor(t, src, libs, store, stack)
	_, err := planAndApply(exec)
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	byAddr := map[string]*state.Entry{}
	for _, ent := range snap.Entries {
		byAddr[ent.Address] = ent
	}
	require.Contains(t, byAddr, "resource.base")
	require.Contains(t, byAddr, "resource.dependent")
	require.Equal(t, []string{"resource.base"},
		byAddr["resource.dependent"].DependsOn)
	require.Empty(t, byAddr["resource.base"].DependsOn)
}

func TestApplyPlanForEachResource(t *testing.T) {
	src := applyPlanFixture(t, "apply-plan-for-each-resource")
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)
	exec := applyPlanTestExecutor(t, src, libs, store, stack)
	exec.Inputs = map[string]any{"configs": map[string]any{"alpha": int64(1), "beta": int64(2)}}
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
	require.True(t, addrs["resource.many['alpha']"])
	require.True(t, addrs["resource.many['beta']"])
}

func TestApplyPlanForEachAction(t *testing.T) {
	src := applyPlanFixture(t, "apply-plan-for-each-action")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := testModules()
	exec := applyPlanTestExecutor(t, src, libs, store, stack)
	exec.Inputs = map[string]any{
		"names": map[string]any{"alpha": "hello-alpha", "beta": "hello-beta"},
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
	require.True(t, addrs["action.many['alpha']"])
	require.True(t, addrs["action.many['beta']"])
}

func TestApplyPlanForEachActionSkipsUnchanged(t *testing.T) {
	src := applyPlanFixture(t, "apply-plan-for-each-action-skips-unchanged")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	var runs int64
	libs := countingModules(&runs)
	inputs := map[string]any{
		"names": map[string]any{"alpha": "first", "beta": "second"},
	}
	apply := func() {
		exec := applyPlanTestExecutor(t, src, libs, store, stack)
		exec.Inputs = inputs
		applyOnce(t, exec)
	}
	apply()
	require.Equal(t, int64(2), atomic.LoadInt64(&runs))
	apply()
	require.Equal(t, int64(2), atomic.LoadInt64(&runs),
		"second apply skips both instances because their trigger hashes match")
}

func TestApplyPlanForEachData(t *testing.T) {
	src := applyPlanFixture(t, "apply-plan-for-each-data")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := testModules()
	exec := applyPlanTestExecutor(t, src, libs, store, stack)
	exec.Inputs = map[string]any{
		"keys": map[string]any{"alpha": "alpha-key", "beta": "beta-key"},
	}
	res, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, "looked-up:alpha-key", res.Outputs["alpha-value"])
	require.Equal(t, "looked-up:beta-key", res.Outputs["beta-value"])
}

func TestApplyPlanCreatesResource(t *testing.T) {
	src := applyPlanFixture(t, "apply-plan-creates-resource")
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)

	exec := applyPlanTestExecutor(t, src, libs, store, stack)
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
	src := applyPlanFixture(t, "apply-plan-persists-create-before-later-failure")
	store := newStateStore(t)
	var c incrementalResourceCounters

	err := applyIncrementalPlan(t, store, &c, src)
	require.ErrorIs(t, err, errIncrementalResource)

	snap, err := store.Current()
	require.NoError(t, err)
	first := snap.Find("resource.first")
	requireIncrementalOutputs(t, first, "first", 1)
	require.Nil(t, snap.Find("resource.later"))
}

func TestApplyPlanPersistsUpdateBeforeLaterFailure(t *testing.T) {
	src := applyPlanFixture(t, "apply-plan-persists-update-before-later-failure")
	store := newStateStore(t)
	seedIncrementalState(t, store,
		incrementalEntry("resource.first", "first", 1))
	var c incrementalResourceCounters

	err := applyIncrementalPlan(t, store, &c, src)
	require.ErrorIs(t, err, errIncrementalResource)

	snap, err := store.Current()
	require.NoError(t, err)
	first := snap.Find("resource.first")
	requireIncrementalOutputs(t, first, "first", 2)
	require.Equal(t, "first", first.Inputs["name"])
	require.EqualValues(t, 2, first.Inputs["size"])
	require.Nil(t, snap.Find("resource.later"))
}

func TestApplyPlanPersistsReplaceBeforeLaterFailure(t *testing.T) {
	src := applyPlanFixture(t, "apply-plan-persists-replace-before-later-failure")
	store := newStateStore(t)
	seedIncrementalState(t, store,
		incrementalEntry("resource.first", "old", 1))
	var c incrementalResourceCounters

	err := applyIncrementalPlan(t, store, &c, src)
	require.ErrorIs(t, err, errIncrementalResource)

	snap, err := store.Current()
	require.NoError(t, err)
	first := snap.Find("resource.first")
	requireIncrementalOutputs(t, first, "new", 1)
	require.Equal(t, "new", first.Inputs["name"])
	require.EqualValues(t, 1, first.Inputs["size"])
	require.Nil(t, snap.Find("resource.later"))
}

func TestApplyPlanPersistsDestroyBeforeLaterFailure(t *testing.T) {
	src := `description: 'empty'`
	store := newStateStore(t)
	seedIncrementalState(t, store,
		incrementalEntry("resource.orphan", "orphan", 1),
		incrementalEntry("resource.later", "fail-delete", 1))
	var c incrementalResourceCounters

	err := applyIncrementalPlan(t, store, &c, src)
	require.ErrorIs(t, err, errIncrementalResource)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Nil(t, snap.Find("resource.orphan"))
	require.NotNil(t, snap.Find("resource.later"))
}

func TestApplyPlanForEachComposite(t *testing.T) {
	composite := syntaxResourceComposite(t, "box",
		applyPlanFixture(t, "apply-plan-for-each-composite-1"))
	var c resourceCounters
	libs := resourceModules(&c)
	composite.Libraries = libs
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": composite,
		},
	}
	src := applyPlanFixture(t, "apply-plan-for-each-composite-2")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := applyPlanTestExecutor(t, src, libs, store, stack)
	exec.Inputs = map[string]any{
		"configs": map[string]any{"alpha": int64(1), "beta": int64(2)},
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
	require.Equal(t, state.EntryLibraryCall, addrs["resource.many['alpha']"])
	require.Equal(t, state.EntryLibraryCall, addrs["resource.many['beta']"])
	require.Equal(t, state.EntryLeaf, addrs["resource.many['alpha']/resource.only"])
	require.Equal(t, state.EntryLeaf, addrs["resource.many['beta']/resource.only"])
}

func TestApplyPlanForEachCompositeOrphan(t *testing.T) {
	composite := syntaxResourceComposite(t, "box",
		applyPlanFixture(t, "apply-plan-for-each-composite-orphan-1"))
	var c resourceCounters
	libs := resourceModules(&c)
	composite.Libraries = libs
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": composite,
		},
	}
	src := applyPlanFixture(t, "apply-plan-for-each-composite-orphan-2")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	apply := func(configs map[string]any) {
		exec := applyPlanTestExecutor(t, src, libs, store, stack)
		exec.Inputs = map[string]any{"configs": configs}
		applyOnce(t, exec)
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
	require.True(t, addrs["resource.many['alpha']"])
	require.True(t, addrs["resource.many['alpha']/resource.only"])
	require.False(t, addrs["resource.many['beta']"])
	require.False(t, addrs["resource.many['beta']/resource.only"])
}

func TestApplyPlanCompositeUsesSyntaxBody(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t,
		applyPlanFixture(t, "apply-plan-composite-uses-syntax-body-1"))
	body := composite.body
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": {Name: "box", SyntaxBody: &body, Libraries: libs},
		},
	}
	root := parseSyntaxFactoryFixture(t,
		applyPlanFixture(t, "apply-plan-composite-uses-syntax-body-2"))
	store := newStateStore(t)
	exec := &Executor{
		DAG:       BuildSyntaxDAG(root.body, libs),
		Libraries: libs,
		Store:     store,
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	res, err := exec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, "fake-alpha-ok", res.Outputs["out"])
	require.Equal(t, int64(1), c.creates)

	snap, err := store.Current()
	require.NoError(t, err)
	addresses := make([]string, len(snap.Entries))
	types := make([]state.EntryType, len(snap.Entries))
	for i, e := range snap.Entries {
		addresses[i] = e.Address
		types[i] = e.Type
	}
	require.ElementsMatch(t, []string{"resource.x", "resource.x/resource.one"}, addresses)
	require.ElementsMatch(t, []state.EntryType{state.EntryLibraryCall, state.EntryLeaf}, types)
}

func TestApplyPlanComposite(t *testing.T) {
	composite := syntaxResourceComposite(t, "box", applyPlanFixture(t, "apply-plan-composite-1"))
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": composite,
		},
	}
	src := applyPlanFixture(t, "apply-plan-composite-2")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := applyPlanTestExecutor(t, src, libs, store, stack)
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
		[]string{"resource.x", "resource.x/resource.one"},
		addresses)
	require.Contains(t, types, state.EntryLibraryCall)
	require.Contains(t, types, state.EntryLeaf)
}

func TestApplyPlanNestedComposite(t *testing.T) {
	clusterBody := syntaxResourceComposite(t, "cluster",
		applyPlanFixture(t, "apply-plan-nested-composite-1"))
	layerBody := syntaxResourceComposite(t, "layer",
		applyPlanFixture(t, "apply-plan-nested-composite-2"))
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
	src := applyPlanFixture(t, "apply-plan-nested-composite-3")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	planExec := applyPlanTestExecutor(t, src, libs, store, stack)
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
	applyExec := applyPlanTestExecutor(t, src, libs, store, stack)
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
			"resource.mine",
			"resource.mine/resource.only",
			"resource.mine/resource.only/resource.x",
		},
		addresses,
		"both boundaries persist as library-call records, plus the deepest leaf")
}

func TestApplyPlanCompositeOrphan(t *testing.T) {
	composite := syntaxResourceComposite(t, "box",
		applyPlanFixture(t, "apply-plan-composite-orphan-1"))
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": composite,
		},
	}
	first := applyPlanFixture(t, "apply-plan-composite-orphan-2")
	second := applyPlanFixture(t, "apply-plan-composite-orphan-3")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	planAndApply := func(src string) *Plan {
		exec := applyPlanTestExecutor(t, src, libs, store, stack)
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
			"resource.x/resource.one",
			"resource.x/resource.two",
		},
		destroyed,
		"the plan reports both internals as destroys")

	snap, err := store.Current()
	require.NoError(t, err)
	addresses := []string{}
	for _, e := range snap.Entries {
		addresses = append(addresses, e.Address)
	}
	require.Equal(t, []string{"resource.keep"}, addresses,
		"only the root-level resource that stays in source remains in state")
}

func TestApplyPlanCompositeWithRootVarArgs(t *testing.T) {
	// The plan and apply phases run separately and apply does not
	// have access to the root inputs that plan used. The composite
	// boundary's args are evaluated at plan time and must seed the
	// composite scope at apply time so internals can read them.
	composite := syntaxResourceComposite(t, "hello",
		applyPlanFixture(t, "apply-plan-composite-with-root-var-args-1"))
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"hello": composite,
		},
	}
	src := applyPlanFixture(t, "apply-plan-composite-with-root-var-args-2")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	planExec := applyPlanTestExecutor(t, src, libs, store, stack)
	planExec.Inputs = map[string]any{"who": "world"}
	plan, err := planExec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	// Apply runs without root inputs, mirroring the stack binary's
	// `apply` subcommand which reads only the plan file.
	applyExec := applyPlanTestExecutor(t, src, libs, store, stack)
	_, err = applyExec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, int64(1), c.creates)

	snap, err := store.Current()
	require.NoError(t, err)
	leaf := findEntryByAddr(snap, "resource.x/resource.greet")
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
	composite := syntaxResourceComposite(t, "box",
		applyPlanFixture(t, "apply-plan-composite-update-in-place-1"))
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": composite,
		},
	}
	first := applyPlanFixture(t, "apply-plan-composite-update-in-place-2")
	second := applyPlanFixture(t, "apply-plan-composite-update-in-place-3")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	planAndApply := func(src string) {
		exec := applyPlanTestExecutor(t, src, libs, store, stack)
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
	src := applyPlanFixture(t, "apply-plan-refuses-on-state-rev-drift")
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)

	exec := applyPlanTestExecutor(t, src, libs, store, stack)
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	// Drift: a separate apply changes state out from under our plan.
	applyOnce(t, applyPlanTestExecutor(t, src, libs, store, stack))

	_, err = exec.ApplyPlan(context.Background(), pf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "state-rev drift")
}

func TestApplyPlanWaitsForLock(t *testing.T) {
	src := applyPlanFixture(t, "apply-plan-waits-for-lock")
	var c resourceCounters
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	libs := resourceModules(&c)
	exec := applyPlanTestExecutor(t, src, libs, store, stack)
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

	exec := applyPlanTestExecutor(t, src, map[string]*Library{}, store, stack)
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
		StateMoves: []PlannedEntryMove{
			{From: "resource.old", To: "resource.x"},
		},
		Steps: []*PlanStep{
			{
				Address:  "resource.x",
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
	require.Equal(t, plan.StateMoves, pf.StateMoves)
	require.Equal(t, "resource.x", pf.Steps[0].Address)
	require.Equal(t, DecisionCreate, pf.Steps[0].Decision)
}

func TestActionRerunsWhenTriggerSourceChanges(t *testing.T) {
	src := func(name string) string {
		return fmt.Sprintf(`%s: {
  one: core.thing { name: '%s', size: 1 }
}
%s: {
  observe: core.echo {
    @trigger: resource.one.id
    echo:     'observed'
  }
}
`, "resources", name, "actions")
	}

	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	var resCounters resourceCounters
	var actionRuns int64
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[countingResource, any, any](
					func() *countingResource {
						return &countingResource{counters: &resCounters}
					},
				),
			},
			Actions: map[string]ActionRegistration{
				"echo": MakeActionWith[countingAction, any, any](
					func() *countingAction {
						return &countingAction{runs: &actionRuns}
					},
				),
			},
		},
	}

	planAndApply := func(stackSrc string) {
		exec := applyPlanTestExecutor(t, stackSrc, libs, store, stack)
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

func TestEncodePlanUsesNodeKindKey(t *testing.T) {
	plan := &Plan{
		Factory: state.FactoryInfo{Name: "x", Version: "v1", ContentRevision: "abc"},
		Steps: []*PlanStep{{
			Address:  "resource.x",
			Kind:     NodeResource,
			Decision: DecisionCreate,
		}},
	}
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"node-kind": "resource"`)
	require.NotContains(t, string(encoded), `"kind": "resource"`)
}

func TestEncodePlanUsesDeferredConfig(t *testing.T) {
	plan := &Plan{
		Factory: state.FactoryInfo{Name: "x", Version: "v1", ContentRevision: "abc"},
		Steps: []*PlanStep{{
			Address:        "data.lookup",
			Kind:           NodeData,
			Decision:       DecisionNoOp,
			DeferredConfig: "library-config.aws",
		}},
	}
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)

	var got struct {
		Steps []map[string]any `json:"steps"`
	}
	require.NoError(t, json.Unmarshal(encoded, &got))
	require.Equal(t, "library-config.aws", got.Steps[0]["deferred-config"])
}

func TestDecodePlanReadsDeferredConfig(t *testing.T) {
	b := []byte(`{
  "format-version": 1,
  "factory": {"name": "x", "version": "v1", "content-revision": "abc"},
  "steps": [{
    "address": "data.lookup",
    "node-kind": "data",
    "decision": "no-op",
    "deferred-config": "library-config.aws"
  }]
}`)
	pf, err := DecodePlan(b)
	require.NoError(t, err)
	require.Equal(t, "library-config.aws", pf.Steps[0].DeferredConfig)
}

func TestDecodePlanRejectsBadFormatVersion(t *testing.T) {
	bad := []byte(`{"format-version": 99, "factory": {"name": "x"}, "steps": []}`)
	_, err := DecodePlan(bad)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported format-version")
}
