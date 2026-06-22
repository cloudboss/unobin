package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/ubtest"
)

// A composite called inside a @for-each composite, with a real
// resource inside and outputs chained through both boundaries.
func TestForEachCompositeCallingComposite(t *testing.T) {
	inner := syntaxResourceComposite(t, "inner",
		ubtest.ReadValidFixture(t, "testdata/ub/nested-foreach-composite", "inner-resource"))
	outer := syntaxResourceComposite(t, "outer",
		ubtest.ReadValidFixture(t, "testdata/ub/nested-foreach-composite", "outer-resource"))
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet": MakeResource[subnetLike, any, any](),
			},
		},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*CompositeType{
				"inner": inner,
				"outer": outer,
			},
		},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/nested-foreach-composite", "resource-call")
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
	res, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, "subnet-1", res.Outputs["ida"])
	require.Equal(t, "subnet-1", res.Outputs["idb"])

	// The second plan diffs every instance's subnet through both
	// boundaries against seeded values, so nothing shows as a change.
	second := &Executor{
		DAG:          exec.DAG,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        store,
		Factory:      stack,
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	for _, key := range []string{"a", "b"} {
		addr := "resource.x['" + key + "']/resource.i/resource.s"
		require.Equal(t, DecisionNoOp, findStep(t, plan, addr).Decision)
	}
	for _, s := range plan.Steps {
		require.Contains(t, []Decision{DecisionNoOp, DecisionEval}, s.Decision,
			"unchanged stack must not plan work for %s", s.Address)
	}
}

// The minimal form: an outputs-only data composite, the one composite
// kind valid with no internals, so nothing needs its scope before the
// boundary finalizes. The outer's outputs read it through the
// per-instance scope.
func TestForEachCompositeCallingDataComposite(t *testing.T) {
	label := syntaxComposite(t, "label", NodeDataSource,
		ubtest.ReadValidFixture(t, "testdata/ub/nested-foreach-composite", "data-label"))
	outer := syntaxResourceComposite(t, "outer",
		ubtest.ReadValidFixture(t, "testdata/ub/nested-foreach-composite", "outer-data-resource"))
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet": MakeResource[subnetLike, any, any](),
			},
		},
		"w": {
			Name: "w",
			DataComposites: map[string]*CompositeType{
				"label": label,
			},
			ResourceComposites: map[string]*CompositeType{
				"outer": outer,
			},
		},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/nested-foreach-composite", "data-call")
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
	res, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, "fixed", res.Outputs["m"])
}
