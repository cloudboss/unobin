package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWalkVisitsNestedExpressions(t *testing.T) {
	f, err := ParseSource("stack.ub", []byte(`
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

func TestWalkVisitsDotPathIndexExpr(t *testing.T) {
	f, err := ParseSource("stack.ub", []byte(`
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
