package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// subnetLike has a tag input and a stable id output; updating it
// never changes the id, like a cloud subnet whose tags are synced.
type subnetLike struct {
	Tag string
}

func (r *subnetLike) SchemaVersion() int { return 1 }

func (r *subnetLike) Create(_ context.Context, _ any) (any, error) {
	return map[string]any{"tag": r.Tag, "id": "subnet-1"}, nil
}

func (r *subnetLike) Read(_ context.Context, _, prior any) (any, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *subnetLike) Update(_ context.Context, _ any, _ Prior[subnetLike, any]) (any, error) {
	return map[string]any{"tag": r.Tag, "id": "subnet-1"}, nil
}

func (r *subnetLike) Delete(_ context.Context, _, _ any) error { return nil }
func (r *subnetLike) ReplaceFields() []string                  { return nil }

// instanceLike replace-marks its ref field, like an instance pinned
// to a subnet id.
type instanceLike struct {
	Ref string
}

func (r *instanceLike) SchemaVersion() int { return 1 }

func (r *instanceLike) Create(_ context.Context, _ any) (any, error) {
	return map[string]any{"ref": r.Ref, "id": "inst-1"}, nil
}

func (r *instanceLike) Read(_ context.Context, _, prior any) (any, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *instanceLike) Update(_ context.Context, _ any, _ Prior[instanceLike, any]) (any, error) {
	return map[string]any{"ref": r.Ref, "id": "inst-1"}, nil
}

func (r *instanceLike) Delete(_ context.Context, _, _ any) error { return nil }
func (r *instanceLike) ReplaceFields() []string                  { return []string{"ref"} }

// pinnedResource replace-marks its tag and mints a fresh id on every
// create, so replacing it hands downstream readers a new value. Its
// update never runs: any input change forces a replace.
type pinnedResource struct {
	Tag string

	gen *int64
}

func (r *pinnedResource) SchemaVersion() int { return 1 }

func (r *pinnedResource) Create(_ context.Context, _ any) (any, error) {
	*r.gen++
	return map[string]any{"tag": r.Tag, "id": fmt.Sprintf("gen-%d", *r.gen)}, nil
}

func (r *pinnedResource) Read(_ context.Context, _, prior any) (any, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *pinnedResource) Update(
	_ context.Context, _ any, _ Prior[pinnedResource, any],
) (any, error) {
	return nil, errors.New("pinnedResource update should never run")
}

func (r *pinnedResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *pinnedResource) ReplaceFields() []string                  { return []string{"tag"} }

// An update preserves the object, so its prior outputs stay readable
// during the plan: a tag sync upstream diffs the downstream against
// the still-valid id and plans no-op instead of a replace.
func TestUpdateKeepsPriorOutputsSeeded(t *testing.T) {
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet":   MakeResource[subnetLike, any, any](),
				"instance": MakeResource[instanceLike, any, any](),
			},
		},
	}
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "cascade-update"), libs)

	applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "1"},
	})

	second := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "2"},
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionUpdate, findStep(t, plan, "resource.a").Decision)
	inst := findStep(t, plan, "resource.it")
	require.Equal(t, DecisionNoOp, inst.Decision)
	require.Empty(t, inst.UnresolvedInputs)
	require.Equal(t, "subnet-1", inst.Inputs["ref"])

	_, err = planAndApplyExisting(second, plan)
	require.NoError(t, err)

	third := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "2"},
	}
	plan, err = third.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, findStep(t, plan, "resource.a").Decision)
	require.Equal(t, DecisionNoOp, findStep(t, plan, "resource.it").Decision)
}

// A replace regenerates the object, so its prior outputs are not
// seeded: a downstream reader of a replace-marked field stays pending
// and plans its own replace, then applies with the fresh value.
func TestReplaceSuppressesPriorOutputs(t *testing.T) {
	var gen int64
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"pinned": MakeResourceWith[pinnedResource, any, any](
					func() *pinnedResource { return &pinnedResource{gen: &gen} },
				),
				"instance": MakeResource[instanceLike, any, any](),
			},
		},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "replace-suppresses")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t, src, libs)

	applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "1"},
	})

	second := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "2"},
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionReplace, findStep(t, plan, "resource.a").Decision)
	inst := findStep(t, plan, "resource.it")
	require.Equal(t, DecisionReplace, inst.Decision)
	require.Contains(t, inst.UnresolvedInputs, "ref")

	_, err = planAndApplyExisting(second, plan)
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.it")
	require.NotNil(t, ent)
	require.Equal(t, map[string]any{"ref": "gen-2"}, ent.Inputs)
}

// A composite's outputs are evaluated during the walk from what its
// internals seeded, so a reader of a composite output diffs a real
// value: an unchanged stack plans no-op instead of replacing the
// reader on every plan.
func TestCompositeOutputsSeedAtPlan(t *testing.T) {
	composite := syntaxResourceComposite(t, "net",
		ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "composite-net"))
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet":   MakeResource[subnetLike, any, any](),
				"instance": MakeResource[instanceLike, any, any](),
			},
		},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*CompositeType{
				"net": composite,
			},
		},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "composite-output-seed")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t, src, libs)
	applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	})

	second := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	inst := findStep(t, plan, "resource.it")
	require.Equal(t, DecisionNoOp, inst.Decision)
	require.Empty(t, inst.UnresolvedInputs)
	require.Equal(t, "subnet-1", inst.Inputs["ref"])
}

// A composite output reading an internal about to be replaced stays
// pending: the suppression of the internal's prior outputs reaches
// through the boundary, so the reader plans its own replace and
// applies with the fresh value.
func TestCompositeOutputPendingWhenInternalReplaces(t *testing.T) {
	var gen int64
	composite := syntaxResourceComposite(t, "net",
		ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "composite-pinned"))
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"pinned": MakeResourceWith[pinnedResource, any, any](
					func() *pinnedResource { return &pinnedResource{gen: &gen} },
				),
				"instance": MakeResource[instanceLike, any, any](),
			},
		},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*CompositeType{
				"net": composite,
			},
		},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "composite-pending")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t, src, libs)
	applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "1"},
	})

	second := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "2"},
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionReplace,
		findStep(t, plan, "resource.x/resource.p").Decision)
	inst := findStep(t, plan, "resource.it")
	require.Equal(t, DecisionReplace, inst.Decision)
	require.Contains(t, inst.UnresolvedInputs, "ref")

	_, err = planAndApplyExisting(second, plan)
	require.NoError(t, err)
	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.it")
	require.NotNil(t, ent)
	require.Equal(t, map[string]any{"ref": "gen-2"}, ent.Inputs)
}

// A slash inside an instance key is not a nesting delimiter.
func TestForEachKeyWithSlashSeedsPriorOutputs(t *testing.T) {
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet":   MakeResource[subnetLike, any, any](),
				"instance": MakeResource[instanceLike, any, any](),
			},
		},
	}
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "foreach-key-slash"), libs)

	applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	})

	second := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, findStep(t, plan, `resource.x['a/b']`).Decision)
	inst := findStep(t, plan, "resource.it")
	require.Equal(t, DecisionNoOp, inst.Decision)
	require.Empty(t, inst.UnresolvedInputs)
	require.Equal(t, "subnet-1", inst.Inputs["ref"])
}

// Each instance of a @for-each composite seeds its own outputs at its
// keyed address, so a reader of one instance's output diffs a real
// value on the second plan.
func TestForEachCompositeOutputsSeedAtPlan(t *testing.T) {
	composite := syntaxResourceComposite(t, "net",
		ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "composite-net"))
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet":   MakeResource[subnetLike, any, any](),
				"instance": MakeResource[instanceLike, any, any](),
			},
		},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*CompositeType{
				"net": composite,
			},
		},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "foreach-composite-output")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t, src, libs)
	applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	})

	second := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	inst := findStep(t, plan, "resource.it")
	require.Equal(t, DecisionNoOp, inst.Decision)
	require.Empty(t, inst.UnresolvedInputs)
	require.Equal(t, "subnet-1", inst.Inputs["ref"])
}
