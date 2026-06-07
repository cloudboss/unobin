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

// A @for-each leaf inside a @for-each composite has no inner fan-out;
// the compile check refuses it, naming the node.
func TestForEachLeafInsideForEachCompositeRejected(t *testing.T) {
	composite := parseStack(t, `
inputs: { tags: { type: map(string) } }
resources: {
  core: {
    subnet: {
      it: {
        @for-each: var.tags
        tag:       @each.value
      }
    }
  }
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
				"outer": {Name: "outer", Body: composite},
			},
		},
	}
	src := `
resources: {
  w: { outer: { x: {
    @for-each: { a: 'one' }
    tags: { t1: 'one' }
  } } }
}
`
	errs := CheckForEachNesting(parseStack(t, src), libs)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "resource.w.outer.x/resource.core.subnet.it")
	require.Contains(t, errs.Err().Error(),
		"@for-each inside a @for-each composite is not supported")
}

// A @for-each composite call inside a @for-each composite is refused
// the same way.
func TestForEachCompositeInsideForEachCompositeRejected(t *testing.T) {
	inner := parseStack(t, `
inputs: { tag: { type: string } }
resources: {
  core: { subnet: { s: { tag: var.tag } } }
}
`)
	outer := parseStack(t, `
inputs: { tags: { type: map(string) } }
resources: {
  w: {
    inner: {
      i: {
        @for-each: var.tags
        tag:       @each.value
      }
    }
  }
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
    @for-each: { a: 'one' }
    tags: { t1: 'one' }
  } } }
}
`
	errs := CheckForEachNesting(parseStack(t, src), libs)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "resource.w.outer.x/resource.w.inner.i")
	require.Contains(t, errs.Err().Error(),
		"@for-each inside a @for-each composite is not supported")
}

// Iteration that does not nest stays legal: a @for-each call with
// plain internals, and a plain composite holding a @for-each leaf.
func TestForEachWithoutNestingPasses(t *testing.T) {
	composite := parseStack(t, `
inputs: { tag: { type: string } }
resources: {
  core: { subnet: { s: { tag: var.tag } } }
}
`)
	plainWithForEach := parseStack(t, `
inputs: { tags: { type: map(string) } }
resources: {
  core: {
    subnet: {
      it: {
        @for-each: var.tags
        tag:       @each.value
      }
    }
  }
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
				"slice": {Name: "slice", Body: composite},
				"plain": {Name: "plain", Body: plainWithForEach},
			},
		},
	}
	src := `
resources: {
  w: {
    slice: { x: {
      @for-each: { a: 'one' }
      tag: @each.value
    } }
    plain: { y: { tags: { t1: 'one' } } }
  }
}
`
	errs := CheckForEachNesting(parseStack(t, src), libs)
	require.Equal(t, 0, errs.Len())
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
