package check

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/runtime"
)

// A @for-each leaf inside a @for-each composite has no inner fan-out;
// the compile check refuses it, naming the node.
func TestForEachLeafInsideForEachCompositeRejected(t *testing.T) {
	composite := parseStack(t, `
inputs:    { tags: { type: map(string) } }
resources: { core.subnet.it: { @for-each: var.tags, tag: @each.value } }
`)
	libs := map[string]*runtime.Library{
		"core": {Name: "core"},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*runtime.CompositeType{
				"outer": {Name: "outer", Body: composite},
			},
		},
	}
	src := `
resources: { w.outer.x: { @for-each: { a: 'one' }, tags: { t1: 'one' } } }
`
	errs := newGenericChecker(parseStack(t, src), libs).ForEachNesting()
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "resource.w.outer.x/resource.core.subnet.it")
	require.Contains(t, errs.Err().Error(),
		"@for-each inside a @for-each composite is not supported")
}

// A @for-each composite call inside a @for-each composite is refused
// the same way.
func TestForEachCompositeInsideForEachCompositeRejected(t *testing.T) {
	inner := parseStack(t, `
inputs:    { tag: { type: string } }
resources: { core.subnet.s: { tag: var.tag } }
`)
	outer := parseStack(t, `
inputs:    { tags: { type: map(string) } }
resources: { w.inner.i: { @for-each: var.tags, tag: @each.value } }
`)
	libs := map[string]*runtime.Library{
		"core": {Name: "core"},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*runtime.CompositeType{
				"inner": {Name: "inner", Body: inner},
				"outer": {Name: "outer", Body: outer},
			},
		},
	}
	src := `
resources: { w.outer.x: { @for-each: { a: 'one' }, tags: { t1: 'one' } } }
`
	errs := newGenericChecker(parseStack(t, src), libs).ForEachNesting()
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "resource.w.outer.x/resource.w.inner.i")
	require.Contains(t, errs.Err().Error(),
		"@for-each inside a @for-each composite is not supported")
}

// Iteration that does not nest stays legal: a @for-each call with
// plain internals, and a plain composite holding a @for-each leaf.
func TestForEachWithoutNestingPasses(t *testing.T) {
	composite := parseStack(t, `
inputs:    { tag: { type: string } }
resources: { core.subnet.s: { tag: var.tag } }
`)
	plainWithForEach := parseStack(t, `
inputs:    { tags: { type: map(string) } }
resources: { core.subnet.it: { @for-each: var.tags, tag: @each.value } }
`)
	libs := map[string]*runtime.Library{
		"core": {Name: "core"},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*runtime.CompositeType{
				"slice": {Name: "slice", Body: composite},
				"plain": {Name: "plain", Body: plainWithForEach},
			},
		},
	}
	src := `
resources: {
  w.slice.x: { @for-each: { a: 'one' }, tag: @each.value }
  w.plain.y: { tags: { t1: 'one' } }
}
`
	errs := newGenericChecker(parseStack(t, src), libs).ForEachNesting()
	require.Equal(t, 0, errs.Len())
}
