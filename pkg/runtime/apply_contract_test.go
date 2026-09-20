package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
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

type ghostResourceOutput struct {
	Tag string
	ID  string
}

func (r *ghostResource) Create(_ context.Context, _ any) (*ghostResourceOutput, error) {
	*r.gen++
	return &ghostResourceOutput{Tag: r.Tag, ID: fmt.Sprintf("gen-%d", *r.gen)}, nil
}

func (r *ghostResource) Read(
	_ context.Context, _ any, prior Prior[ghostResource, *ghostResourceOutput, any],
) (*ghostResourceOutput, error) {
	if *r.gone || prior.Outputs == nil {
		return nil, ErrNotFound
	}
	return prior.Outputs, nil
}

func (r *ghostResource) Update(
	_ context.Context, _ any, _ Prior[ghostResource, *ghostResourceOutput, any],
) (*ghostResourceOutput, error) {
	*r.gen++
	return &ghostResourceOutput{Tag: r.Tag, ID: fmt.Sprintf("gen-%d", *r.gen)}, nil
}

func (r *ghostResource) Delete(
	_ context.Context, _ any, _ Prior[ghostResource, *ghostResourceOutput, any],
) error {
	return nil
}

// An input that reads an output from a resource being recreated remains
// pending until apply. The dependent then evaluates against the new output
// and updates in the same apply.
func TestApplyResolvesInputAfterGoneDependencyRecreated(t *testing.T) {
	var gen int64
	gone := false
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"ghost": MakeResourceWith[ghostResource, *ghostResourceOutput, any](
					testResourceDefinition[ghostResource, *ghostResourceOutput, any](),
					func() *ghostResource { return &ghostResource{gen: &gen, gone: &gone} },
				),
				"thing": MakeResource[trackedResource, *trackedResourceOutput, any](
					testResourceDefinition[trackedResource, *trackedResourceOutput, any](),
				),
			},
		},
	}
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-contract", "ghost-dep"), libs)

	applyOnce(t, &Executor{
		DAG:          g,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        store,
		Factory:      stack,
	})

	// The plan sees the upstream gone and leaves its output unavailable to
	// the downstream until Create returns the new value.
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
	downstream := findStep(t, plan, "resource.two")
	require.Equal(t, DecisionUpdate, downstream.Decision)
	require.Contains(t, downstream.UnresolvedInputs, "tag")
	require.IsType(t, PendingValue{}, downstream.Inputs["tag"])

	// The recreate mints gen-2, which the downstream consumes when apply
	// resolves its pending input.
	gone = false
	_, err = planAndApplyExisting(second, plan)
	require.NoError(t, err)

	// Both resources converge in the same apply.
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
	require.Equal(t, DecisionNoOp, findStep(t, plan, "resource.two").Decision)
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
				"ghost": MakeResourceWith[ghostResource, *ghostResourceOutput, any](
					testResourceDefinition[ghostResource, *ghostResourceOutput, any](),
					func() *ghostResource { return &ghostResource{gen: &gen, gone: &gone} },
				),
				"thing": MakeResource[trackedResource, *trackedResourceOutput, any](
					testResourceDefinition[trackedResource, *trackedResourceOutput, any](),
				),
			},
		},
	}
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-contract", "ghost-dep"), libs)
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
