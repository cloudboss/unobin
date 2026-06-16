package check

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type syntaxRuntimeFixture struct {
	body syntax.FactoryBody
}

func parseSyntaxFactoryFixture(t *testing.T, src string) syntaxRuntimeFixture {
	t.Helper()
	f, err := syntax.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)
	require.NotNil(t, f.Factory)
	return syntaxRuntimeFixture{body: f.Factory.Body}
}

func parseSyntaxCompositeFixture(t *testing.T, src string) syntaxRuntimeFixture {
	t.Helper()
	f, err := syntax.ParseSource("library.ub", []byte(src))
	require.NoError(t, err)
	require.NotNil(t, f.Library)
	require.Len(t, f.Library.Exports, 1)
	return syntaxRuntimeFixture{body: f.Library.Exports[0].Body}
}

func parseStack(t *testing.T, src string) *lang.File {
	t.Helper()
	f, err := lang.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)
	if inputs := lang.TopLevelBlock(f, "inputs"); inputs != nil {
		errs := lang.ValidateInputDeclarations(inputs)
		require.Equal(t, 0, errs.Len(), errs.Error())
	}
	return f
}

func newGenericChecker(f *lang.File, libs map[string]*runtime.Library) *Checker {
	return newChecker(
		f,
		runtime.BuildDAG(f, libs),
		runtime.InputNames(f),
		localNames(f),
		libs,
	)
}

// checkReferences runs the reference check for tests that need only diagnostics.
func checkReferences(f *lang.File, libs map[string]*runtime.Library) *lang.ErrorList {
	return newGenericChecker(f, libs).References(nil)
}

// checkLiteralConstraints mirrors checkReferences for the literal constraint check.
func checkLiteralConstraints(f *lang.File, libs map[string]*runtime.Library) *lang.ErrorList {
	return newGenericChecker(f, libs).LiteralConstraints()
}

func parseValue(t *testing.T, src string) lang.Expr {
	t.Helper()
	f, err := lang.ParseSource("", []byte("v: "+src+"\n"))
	require.NoError(t, err)
	require.Len(t, f.Body.Fields, 1)
	return f.Body.Fields[0].Value
}
