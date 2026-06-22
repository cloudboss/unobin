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

func checkFactorySource(src string) string {
	return "factory" + ": {\n" + src + "\n}"
}

// checkSyntaxReferences runs the typed reference check for tests that need only diagnostics.
func checkSyntaxReferences(
	t *testing.T,
	src string,
	libs map[string]*runtime.Library,
) *lang.ErrorList {
	t.Helper()
	fixture := parseSyntaxFactoryFixture(t, checkFactorySource(src))
	return NewSyntax(fixture.body, libs).References(nil)
}

func parseValue(t *testing.T, src string) lang.Expr {
	t.Helper()
	f, err := lang.ParseSource("", []byte("v: "+src+"\n"))
	require.NoError(t, err)
	require.Len(t, f.Body.Fields, 1)
	return f.Body.Fields[0].Value
}
