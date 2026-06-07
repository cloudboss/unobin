package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

// trackedResource reads back its prior outputs, so an unchanged input
// set diffs to NoOp the way a real cloud resource would.
type trackedResource struct {
	Tag string
}

func (r *trackedResource) SchemaVersion() int { return 1 }

func (r *trackedResource) Create(_ context.Context, _ any) (any, error) {
	return map[string]any{"tag": r.Tag, "id": "id-1"}, nil
}

func (r *trackedResource) Read(_ context.Context, _, prior any) (any, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *trackedResource) Update(
	_ context.Context, _ any, _ Prior[trackedResource, any],
) (any, error) {
	return map[string]any{"tag": r.Tag, "id": "id-1"}, nil
}

func (r *trackedResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *trackedResource) ReplaceFields() []string                  { return nil }

// dialDataSource returns whatever the test dialed in, suffixed with
// the key, and counts reads so a test can pin when reads happen.
type dialDataSource struct {
	Key string

	value *string
	reads *int64
}

func (d *dialDataSource) Read(_ context.Context, _ any) (any, error) {
	atomic.AddInt64(d.reads, 1)
	return map[string]any{"value": *d.value + ":" + d.Key}, nil
}

func dataPlanModules(value *string, reads *int64) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResource[trackedResource, any](),
			},
			DataSources: map[string]DataSourceRegistration{
				"dial": MakeDataSourceWith[dialDataSource, any](
					func() *dialDataSource { return &dialDataSource{value: value, reads: reads} },
				),
			},
		},
	}
}

func findStep(t *testing.T, p *Plan, addr string) *PlanStep {
	t.Helper()
	for _, s := range p.Steps {
		if s.Address == addr {
			return s
		}
	}
	t.Fatalf("plan has no step %q; steps: %v", addr, stepAddresses(p))
	return nil
}

func stepAddresses(p *Plan) []string {
	out := make([]string, 0, len(p.Steps))
	for _, s := range p.Steps {
		out = append(out, s.Address+":"+string(s.Decision))
	}
	return out
}

const dataConsumerSrc = `
data: {
  core: { dial: { cfg: { key: 'k' } } }
}
resources: {
  core: { thing: { one: { tag: data.core.dial.cfg.value } } }
}
outputs: {
  v: { value: data.core.dial.cfg.value }
}
`

// A data source whose inputs resolve at plan is read during the plan,
// so the resource consuming it diffs a real value, not a pending one.
func TestPlanReadsResolvedDataSource(t *testing.T) {
	value := "a"
	var reads int64
	libs := dataPlanModules(&value, &reads)
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, dataConsumerSrc), libs),
		Libraries: libs,
		Store:     newStateStore(t),
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)

	ds := findStep(t, plan, "data.core.dial.cfg")
	require.Equal(t, DecisionRead, ds.Decision)
	require.Equal(t, map[string]any{"value": "a:k"}, ds.ObservedOutputs)
	require.Empty(t, ds.UnresolvedInputs)

	rsStep := findStep(t, plan, "resource.core.thing.one")
	require.Equal(t, DecisionCreate, rsStep.Decision)
	require.Equal(t, "a:k", rsStep.Inputs["tag"])
	require.Empty(t, rsStep.UnresolvedInputs)

	require.Equal(t, int64(1), reads)
}

// The reported bug: a resource fed by a data source was updated or
// replaced on every plan, because the data value stayed pending and
// never compared equal to prior state. With the read at plan, an
// unchanged data value diffs to NoOp.
func TestSecondPlanNoOpWhenDataUnchanged(t *testing.T) {
	value := "a"
	var reads int64
	libs := dataPlanModules(&value, &reads)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g := BuildDAG(parseStack(t, dataConsumerSrc), libs)

	applyOnce(t, &Executor{DAG: g, Libraries: libs, Store: store, Factory: stack})
	require.Equal(t, int64(2), reads, "one read at plan, one verifying read at apply")

	second := &Executor{DAG: g, Libraries: libs, Store: store, Factory: stack}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, findStep(t, plan, "resource.core.thing.one").Decision)
	require.Equal(t, int64(3), reads)
}

// A changed data value flows into a real Update decision.
func TestSecondPlanUpdatesWhenDataChanged(t *testing.T) {
	value := "a"
	var reads int64
	libs := dataPlanModules(&value, &reads)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g := BuildDAG(parseStack(t, dataConsumerSrc), libs)

	applyOnce(t, &Executor{DAG: g, Libraries: libs, Store: store, Factory: stack})
	value = "b"
	second := &Executor{DAG: g, Libraries: libs, Store: store, Factory: stack}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	step := findStep(t, plan, "resource.core.thing.one")
	require.Equal(t, DecisionUpdate, step.Decision)
	require.Equal(t, "b:k", step.Inputs["tag"])
}

// A data source whose input waits on a resource created this plan
// keeps today's behavior: the read defers to apply and everything
// downstream of it stays pending. The apply still resolves end to end.
func TestPlanDefersDataWithPendingInputs(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { tag: 'fixed' } } }
}
data: {
  core: { dial: { cfg: { key: resource.core.thing.one.id } } }
}
outputs: {
  v: { value: data.core.dial.cfg.value }
}
`
	value := "a"
	var reads int64
	libs := dataPlanModules(&value, &reads)
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
	ds := findStep(t, plan, "data.core.dial.cfg")
	require.Equal(t, DecisionRead, ds.Decision)
	require.Nil(t, ds.ObservedOutputs)
	require.Contains(t, ds.UnresolvedInputs, "key")
	require.Equal(t, int64(0), reads, "a deferred data source is not read at plan")

	res := applyOnce(t, exec)
	require.Equal(t, "a:id-1", res.Outputs["v"])
}

// Apply re-reads a plan-read data source and refuses to proceed when
// the value moved: the plan is a contract, and a drifted premise means
// re-plan rather than silently applying something that was never shown.
func TestApplyErrorsWhenDataChangedSincePlan(t *testing.T) {
	value := "a"
	var reads int64
	libs := dataPlanModules(&value, &reads)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, dataConsumerSrc), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	ctx := context.Background()
	plan, err := exec.Plan(ctx)
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	value = "b"
	_, err = exec.ApplyPlan(ctx, pf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "data.core.dial.cfg")
	require.Contains(t, err.Error(), "changed since the plan")
}

// Data reads are recorded in state, like Terraform's data entries, so
// the snapshot shows what the apply consumed; a data node removed from
// source is pruned from state without a destroy step.
func TestDataStoredInStateAndPruned(t *testing.T) {
	value := "a"
	var reads int64
	libs := dataPlanModules(&value, &reads)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g := BuildDAG(parseStack(t, dataConsumerSrc), libs)
	applyOnce(t, &Executor{DAG: g, Libraries: libs, Store: store, Factory: stack})

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("data.core.dial.cfg")
	require.NotNil(t, ent, "the data read belongs in state")
	require.Equal(t, state.EntryData, ent.Type)
	require.Equal(t, "dial", ent.Kind)
	require.Equal(t, map[string]any{"key": "k"}, ent.Inputs)
	require.Equal(t, map[string]any{"value": "a:k"}, ent.Outputs)

	withoutData := `
resources: {
  core: { thing: { one: { tag: 'fixed' } } }
}
`
	second := &Executor{
		DAG:       BuildDAG(parseStack(t, withoutData), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	for _, s := range plan.Steps {
		require.NotEqual(t, "data.core.dial.cfg", s.Address,
			"a removed data node prunes from state without a step")
	}
	_, err = planAndApplyExisting(second, plan)
	require.NoError(t, err)
	snap, err = store.Current()
	require.NoError(t, err)
	require.Nil(t, snap.Find("data.core.dial.cfg"))
}

// Each @for-each instance reads at plan with its own key.
func TestForEachDataReadsAtPlan(t *testing.T) {
	src := `
data: {
  core: { dial: { cfg: {
    @for-each: { a: 'x', b: 'y' }
    key: @each.value
  } } }
}
`
	value := "v"
	var reads int64
	libs := dataPlanModules(&value, &reads)
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     newStateStore(t),
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]any{"value": "v:x"},
		findStep(t, plan, `data.core.dial.cfg['a']`).ObservedOutputs)
	require.Equal(t, map[string]any{"value": "v:y"},
		findStep(t, plan, `data.core.dial.cfg['b']`).ObservedOutputs)
	require.Equal(t, int64(2), reads)
}

// amiOut mimics a cloud data source's richer output: a nested struct
// list whose field order is not alphabetical, an optional field left
// nil, an empty list, and a timestamp.
type amiDevice struct {
	Name string
	Size int64
}

type amiOut struct {
	ID      string
	Devices []amiDevice
	Alias   *string
	Tags    []string
	Created time.Time
}

type amiDataSource struct {
	Key string
}

func (d *amiDataSource) Read(_ context.Context, _ any) (*amiOut, error) {
	return &amiOut{
		ID:      "ami-1",
		Devices: []amiDevice{{Name: "xvda", Size: 8}},
		Tags:    []string{},
		Created: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}, nil
}

// The values a data source returns survive the plan file as plain
// JSON, while a fresh read holds live Go structs; the two must
// compare equal at apply, or an unchanged world fails as changed.
func TestApplyAcceptsUnchangedStructOutputs(t *testing.T) {
	libs := map[string]*Library{
		"core": {
			Name: "core",
			DataSources: map[string]DataSourceRegistration{
				"ami": MakeDataSource[amiDataSource, *amiOut](),
			},
		},
	}
	exec := &Executor{
		DAG: BuildDAG(parseStack(t, `
data: {
  core: { ami: { al: { key: 'k' } } }
}
outputs: {
  id:   { value: data.core.ami.al.id }
  name: { value: data.core.ami.al.devices[0].name }
}
`), libs),
		Libraries: libs,
		Store:     newStateStore(t),
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	res, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, "ami-1", res.Outputs["id"])
	require.Equal(t, "xvda", res.Outputs["name"])

	// A second cycle compares state-decoded values against a fresh
	// read; an unchanged world must still agree with itself.
	res, err = planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, "ami-1", res.Outputs["id"])
}

// A destroy plan removes the data record like the other state-only
// entries, reading nothing.
func TestDestroyRemovesDataEntry(t *testing.T) {
	value := "a"
	var reads int64
	libs := dataPlanModules(&value, &reads)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g := BuildDAG(parseStack(t, dataConsumerSrc), libs)
	applyOnce(t, &Executor{DAG: g, Libraries: libs, Store: store, Factory: stack})
	readsBefore := reads

	destroy := &Executor{
		DAG: g, Libraries: libs, Store: store, Factory: stack, Destroy: true,
	}
	_, err := planAndApply(destroy)
	require.NoError(t, err)
	require.Equal(t, readsBefore, reads, "destroy reads no data sources")
	snap, err := store.Current()
	require.NoError(t, err)
	require.Nil(t, snap.Find("data.core.dial.cfg"))
	require.Nil(t, snap.Find("resource.core.thing.one"))
}

// planAndApplyExisting applies an already computed plan through the
// same encode/decode round trip planAndApply uses.
func planAndApplyExisting(exec *Executor, plan *Plan) (*ExecResult, error) {
	encoded, err := EncodePlan(plan)
	if err != nil {
		return nil, err
	}
	pf, err := DecodePlan(encoded)
	if err != nil {
		return nil, err
	}
	return exec.ApplyPlan(context.Background(), pf)
}
