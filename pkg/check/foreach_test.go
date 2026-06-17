package check

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/runtime"
)

// A @for-each leaf inside a @for-each composite has no inner fan-out;
// the compile check refuses it, naming the node.
func TestForEachLeafInsideForEachCompositeRejected(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
outer: resource {
  inputs:    { tags: { type: map(string) } }
  resources: { it: core.subnet { @for-each: var.tags, tag: @each.value } }
}
`)
	body := composite.body
	libs := map[string]*runtime.Library{
		"core": {Name: "core"},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*runtime.CompositeType{
				"outer": {Name: "outer", SyntaxBody: &body},
			},
		},
	}
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  resources: { x: w.outer { @for-each: { a: 'one' }, tags: { t1: 'one' } } }
}
`)
	errs := NewSyntax(fixture.body, libs).ForEachNesting()
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "resource.x/resource.it")
	require.Contains(t, errs.Err().Error(),
		"@for-each inside a @for-each composite is not supported")
}

// A @for-each composite call inside a @for-each composite is refused
// the same way.
func TestForEachCompositeInsideForEachCompositeRejected(t *testing.T) {
	inner := parseSyntaxCompositeFixture(t, `
inner: resource {
  inputs:    { tag: { type: string } }
  resources: { s: core.subnet { tag: var.tag } }
}
`)
	innerBody := inner.body
	outer := parseSyntaxCompositeFixture(t, `
outer: resource {
  inputs:    { tags: { type: map(string) } }
  resources: { i: w.inner { @for-each: var.tags, tag: @each.value } }
}
`)
	outerBody := outer.body
	libs := map[string]*runtime.Library{
		"core": {Name: "core"},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*runtime.CompositeType{
				"inner": {Name: "inner", SyntaxBody: &innerBody},
				"outer": {Name: "outer", SyntaxBody: &outerBody},
			},
		},
	}
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  resources: { x: w.outer { @for-each: { a: 'one' }, tags: { t1: 'one' } } }
}
`)
	errs := NewSyntax(fixture.body, libs).ForEachNesting()
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "resource.x/resource.i")
	require.Contains(t, errs.Err().Error(),
		"@for-each inside a @for-each composite is not supported")
}

// Iteration that does not nest stays legal: a @for-each call with
// plain internals, and a plain composite holding a @for-each leaf.
func TestForEachWithoutNestingPasses(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
slice: resource {
  inputs:    { tag: { type: string } }
  resources: { s: core.subnet { tag: var.tag } }
}
`)
	compositeBody := composite.body
	plainWithForEach := parseSyntaxCompositeFixture(t, `
plain: resource {
  inputs:    { tags: { type: map(string) } }
  resources: { it: core.subnet { @for-each: var.tags, tag: @each.value } }
}
`)
	plainBody := plainWithForEach.body
	libs := map[string]*runtime.Library{
		"core": {Name: "core"},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*runtime.CompositeType{
				"slice": {Name: "slice", SyntaxBody: &compositeBody},
				"plain": {Name: "plain", SyntaxBody: &plainBody},
			},
		},
	}
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  resources: {
    x: w.slice { @for-each: { a: 'one' }, tag: @each.value }
    y: w.plain { tags: { t1: 'one' } }
  }
}
`)
	errs := NewSyntax(fixture.body, libs).ForEachNesting()
	require.Equal(t, 0, errs.Len())
}
