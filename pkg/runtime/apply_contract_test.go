package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// ghostResource mints a fresh id on every create. Its Read reports
// the resource missing while *gone is set, so a test can simulate a
// resource deleted out of band between applies.
type ghostResource struct {
	Tag string

	gen  *int64
	gone *bool
}

func (r *ghostResource) SchemaVersion() int { return 1 }

func (r *ghostResource) Create(_ context.Context, _ any) (any, error) {
	*r.gen++
	return map[string]any{"tag": r.Tag, "id": fmt.Sprintf("gen-%d", *r.gen)}, nil
}

func (r *ghostResource) Read(_ context.Context, _, prior any) (any, error) {
	if *r.gone || prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *ghostResource) Update(
	_ context.Context, _ any, _ Prior[ghostResource, any],
) (any, error) {
	*r.gen++
	return map[string]any{"tag": r.Tag, "id": fmt.Sprintf("gen-%d", *r.gen)}, nil
}

func (r *ghostResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *ghostResource) ReplaceFields() []string                  { return nil }

// A concrete plan-time input that evaluates differently at apply
// fails the step: the decision was computed from a premise that no
// longer holds, so the answer is a fresh plan. Here the upstream was
// recreated out of band, the plan diffed the downstream against the
// dead id and chose no-op, and apply must refuse rather than write
// the new id into state without running anything.
func TestApplyErrorsWhenResourceInputChangedSincePlan(t *testing.T) {
	var gen int64
	gone := false
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"ghost": MakeResourceWith[ghostResource, any, any](
					func() *ghostResource { return &ghostResource{gen: &gen, gone: &gone} },
				),
				"thing": MakeResource[trackedResource, any, any](),
			},
		},
	}
	src := `
resources: { one: core.ghost { tag: 'x' }, two: core.thing { tag: resource.one.id } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t, src, libs)

	applyOnce(t, &Executor{
		DAG:          g,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        store,
		Factory:      stack,
	})

	// The plan sees the upstream gone and decides to recreate it; the
	// downstream diffs against the seeded prior id and plans no-op.
	gone = true
	second := &Executor{
		DAG:          g,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        store,
		Factory:      stack,
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionCreate, findStep(t, plan, "resource.one").Decision)
	require.Equal(t, DecisionNoOp, findStep(t, plan, "resource.two").Decision)

	// The recreate mints gen-2, so the downstream's tag re-evaluates
	// to a value the plan never showed.
	gone = false
	_, err = planAndApplyExisting(second, plan)
	require.Error(t, err)
	require.Contains(t, err.Error(), "resource.two")
	require.Contains(t, err.Error(), "inputs changed since the plan was computed; plan again")
	require.Contains(t, err.Error(), `tag: "gen-1" -> "gen-2"`,
		"the error names each moved field with both values")

	// The recreate persisted before the failure, so one re-plan diffs
	// the downstream against the new id and converges.
	third := &Executor{
		DAG:          g,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        store,
		Factory:      stack,
	}
	plan, err = third.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, findStep(t, plan, "resource.one").Decision)
	require.Equal(t, DecisionUpdate, findStep(t, plan, "resource.two").Decision)
	_, err = planAndApplyExisting(third, plan)
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.two")
	require.NotNil(t, ent)
	require.Equal(t, map[string]any{"tag": "gen-2"}, ent.Inputs)
}

// A field the plan left unresolved is allowed to settle at apply;
// only fields the plan showed as concrete are held to their value.
func TestApplyAcceptsResolvedPendingInput(t *testing.T) {
	var gen int64
	gone := false
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"ghost": MakeResourceWith[ghostResource, any, any](
					func() *ghostResource { return &ghostResource{gen: &gen, gone: &gone} },
				),
				"thing": MakeResource[trackedResource, any, any](),
			},
		},
	}
	src := `
resources: { one: core.ghost { tag: 'x' }, two: core.thing { tag: resource.one.id } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        store,
		Factory:      stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	step := findStep(t, plan, "resource.two")
	require.Contains(t, step.UnresolvedInputs, "tag")

	_, err = planAndApplyExisting(exec, plan)
	require.NoError(t, err)
}
