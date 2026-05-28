package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWalkVisitsNestedExpressions(t *testing.T) {
	f, err := ParseSource("factory.ub", []byte(`
inputs: {
  size: { type: integer }
}
resources: {
  a: { b: { c: { x: format('%d', var.size + 1) } } }
}
`))
	require.NoError(t, err)

	var dotPaths []string
	Walk(f.Body, func(e Expr) {
		if dp, ok := e.(*DotPath); ok {
			dotPaths = append(dotPaths, dp.Root.Name)
		}
	})
	require.Contains(t, dotPaths, "var")
}

func TestWalkNilIsSafe(t *testing.T) {
	called := false
	Walk(nil, func(Expr) { called = true })
	require.False(t, called)
}

func TestWalkVisitsConditionalBranches(t *testing.T) {
	f, err := ParseSource("factory.ub", []byte(`
resources: {
  a: { b: { c: { x: if var.prod then var.big else var.small } } }
}
`))
	require.NoError(t, err)

	var roots []string
	Walk(f.Body, func(e Expr) {
		if dp, ok := e.(*DotPath); ok && len(dp.Segments) > 0 {
			roots = append(roots, dp.Root.Name+"."+dp.Segments[0].Name)
		}
	})
	require.Contains(t, roots, "var.prod")
	require.Contains(t, roots, "var.big")
	require.Contains(t, roots, "var.small")
}

func TestWalkVisitsComprehensionParts(t *testing.T) {
	f, err := ParseSource("factory.ub", []byte(`
resources: {
  a: { b: { c: { x: [ for s in var.subnets : s.cidr when var.enabled ] } } }
}
`))
	require.NoError(t, err)

	var roots []string
	Walk(f.Body, func(e Expr) {
		if dp, ok := e.(*DotPath); ok {
			roots = append(roots, dp.Root.Name)
		}
	})
	require.Contains(t, roots, "var", "source and filter var refs should be visited")
	require.Contains(t, roots, "s", "bound name in the body should be visited")
}

func TestWalkVisitsDotPathIndexExpr(t *testing.T) {
	f, err := ParseSource("factory.ub", []byte(`
resources: {
  a: { b: { c: { x: resource.aws.thing.many['alpha'].id } } }
}
`))
	require.NoError(t, err)

	var strings []string
	Walk(f.Body, func(e Expr) {
		if s, ok := e.(*StringLit); ok {
			strings = append(strings, s.Value)
		}
	})
	require.Contains(t, strings, "alpha",
		"index expressions on dot paths should be visited")
}
