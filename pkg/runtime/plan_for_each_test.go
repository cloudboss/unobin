package runtime

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func countingInstancesLibrary(evals *int64) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet": MakeResource[subnetLike, any](),
			},
			Functions: map[string]FunctionType{
				"instances": MakeFunc("instances", "Counts evaluations.",
					func() (any, error) {
						atomic.AddInt64(evals, 1)
						return map[string]any{"a": "one", "b": "two", "c": "three"}, nil
					}),
			},
		},
	}
}

// A composite call's @for-each iterable evaluates once per run; each
// instance's scope reuses that evaluation.
func TestForEachCompositeIterableEvaluatesOncePerRun(t *testing.T) {
	inner := syntaxResourceComposite(t, "inner", `
inputs:    { tag: { type: string } }
resources: { s: core.subnet { tag: var.tag } }
`)
	var evals int64
	libs := countingInstancesLibrary(&evals)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"inner": inner,
		},
	}
	src := `
resources: { x: w.inner { @for-each: core.instances(), tag: @each.value } }
`
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	var boundaries []string
	for _, s := range plan.Steps {
		if s.Composite {
			boundaries = append(boundaries, s.Address)
		}
	}
	require.ElementsMatch(t, []string{
		"resource.x['a']",
		"resource.x['b']",
		"resource.x['c']",
	}, boundaries)
	require.Equal(t, int64(1), atomic.LoadInt64(&evals))
}

// A leaf's @for-each iterable evaluates once per run: once during plan
// and once during apply, no matter how many instances it fans into.
func TestForEachLeafIterableEvaluatesOncePerRun(t *testing.T) {
	var evals int64
	libs := countingInstancesLibrary(&evals)
	src := `
resources: { s: core.subnet { @for-each: core.instances(), tag: @each.value } }
`
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	_, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, int64(2), atomic.LoadInt64(&evals))
}
