package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// A composite called inside a @for-each composite, with a real
// resource inside and outputs chained through both boundaries.
func TestForEachCompositeCallingComposite(t *testing.T) {
	inner := parseStack(t, `
inputs: { tag: { type: string } }
resources: {
  core: { subnet: { s: { tag: var.tag } } }
}
outputs: {
  id: { value: resource.core.subnet.s.id }
}
`)
	outer := parseStack(t, `
inputs: { t: { type: string } }
resources: {
  w: { inner: { i: { tag: var.t } } }
}
outputs: {
  id: { value: resource.w.inner.i.id }
}
`)
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet": MakeResource[subnetLike, any](),
			},
		},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*CompositeType{
				"inner": {Name: "inner", Body: inner},
				"outer": {Name: "outer", Body: outer},
			},
		},
	}
	src := `
resources: {
  w: { outer: { x: {
    @for-each: { a: 'one', b: 'two' }
    t: @each.value
  } } }
}
outputs: {
  ida: { value: resource.w.outer.x['a'].id }
  idb: { value: resource.w.outer.x['b'].id }
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
	res, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, "subnet-1", res.Outputs["ida"])
	require.Equal(t, "subnet-1", res.Outputs["idb"])

	// The second plan diffs every instance's subnet through both
	// boundaries against seeded values, so nothing shows as a change.
	second := &Executor{DAG: exec.DAG, Libraries: libs, Store: store, Factory: stack}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	for _, key := range []string{"a", "b"} {
		addr := "resource.w.outer.x['" + key + "']/resource.w.inner.i/resource.core.subnet.s"
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
	label := parseStack(t, `
inputs: { note: { type: string } }
outputs: {
  marker: { value: 'fixed' }
}
`)
	outer := parseStack(t, `
inputs: { t: { type: string } }
data: {
  w: { label: { i: { note: var.t } } }
}
resources: {
  core: { subnet: { s: { tag: var.t } } }
}
outputs: {
  marker: { value: data.w.label.i.marker }
}
`)
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet": MakeResource[subnetLike, any](),
			},
		},
		"w": {
			Name: "w",
			DataComposites: map[string]*CompositeType{
				"label": {Name: "label", Body: label},
			},
			ResourceComposites: map[string]*CompositeType{
				"outer": {Name: "outer", Body: outer},
			},
		},
	}
	src := `
resources: {
  w: { outer: { x: {
    @for-each: { a: 'one' }
    t: @each.value
  } } }
}
outputs: {
  m: { value: resource.w.outer.x['a'].marker }
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
	res, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, "fixed", res.Outputs["m"])
}
