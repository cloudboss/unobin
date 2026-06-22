package lang

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func parseWalkFixture(t *testing.T, name string) *File {
	t.Helper()
	src, err := os.ReadFile("testdata/ub/walk/valid/" + name + ".ub")
	require.NoError(t, err)
	f, err := ParseSource("factory.ub", src)
	require.NoError(t, err)
	return f
}

func TestWalkVisitsNestedExpressions(t *testing.T) {
	f := parseWalkFixture(t, "nested-expressions")

	var dotPaths []string
	Walk(f.Body, func(e Expr) {
		if dp, ok := e.(*DotPath); ok {
			dotPaths = append(dotPaths, dp.Root.Name)
		}
	})
	require.Contains(t, dotPaths, "input")
}

func TestWalkNilIsSafe(t *testing.T) {
	called := false
	Walk(nil, func(Expr) { called = true })
	require.False(t, called)
}

func TestWalkVisitsParsedTypeDeclarations(t *testing.T) {
	f := parseWalkFixture(t, "type-declarations")
	inputs := TopLevelBlock(f, "inputs")
	errs := ValidateInputDeclarations(inputs)
	require.Equal(t, 0, errs.Len(), errs.Error())

	var calls []string
	Walk(f.Body, func(e Expr) {
		if call, ok := e.(*Call); ok && call.Callee != nil {
			calls = append(calls, call.Callee.Name)
		}
	})
	require.Contains(t, calls, "pick")
}

func TestWalkVisitsConditionalBranches(t *testing.T) {
	f := parseWalkFixture(t, "conditional-branches")

	var roots []string
	Walk(f.Body, func(e Expr) {
		if dp, ok := e.(*DotPath); ok && len(dp.Segments) > 0 {
			roots = append(roots, dp.Root.Name+"."+dp.Segments[0].Name)
		}
	})
	require.Contains(t, roots, "input.prod")
	require.Contains(t, roots, "input.big")
	require.Contains(t, roots, "input.small")
}

func TestWalkVisitsComprehensionParts(t *testing.T) {
	f := parseWalkFixture(t, "comprehension-parts")

	var roots []string
	Walk(f.Body, func(e Expr) {
		if dp, ok := e.(*DotPath); ok {
			roots = append(roots, dp.Root.Name)
		}
	})
	require.Contains(t, roots, "input", "source and filter input refs should be visited")
	require.Contains(t, roots, "s", "bound name in the body should be visited")
}

func TestWalkVisitsDotPathIndexExpr(t *testing.T) {
	f := parseWalkFixture(t, "dot-path-index")

	var strings []string
	Walk(f.Body, func(e Expr) {
		if s, ok := e.(*StringLit); ok {
			strings = append(strings, s.Value)
		}
	})
	require.Contains(t, strings, "alpha",
		"index expressions on dot paths should be visited")
}
