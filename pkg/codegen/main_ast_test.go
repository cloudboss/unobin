package codegen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func TestGenerateEmbedsFactorySyntaxBody(t *testing.T) {
	out, err := Generate(testASTInput(t, "demo"))
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, `"github.com/cloudboss/unobin/pkg/lang"`)
	require.Contains(t, s, `"github.com/cloudboss/unobin/pkg/lang/parse"`)
	require.Contains(t, s, `"github.com/cloudboss/unobin/pkg/lang/syntax"`)
	require.Contains(t, s, "factoryBody = syntax.FactoryBody{")
	require.Contains(t, s, "FactoryBody:     &factoryBody,")
	require.NotContains(t, s, "factoryBody = \"")
	require.NotContains(t, s, "syntax.ParseSource")
}

func testASTInput(t *testing.T, factoryName string) Input {
	t.Helper()
	src := []byte(testASTFactorySource())
	sf, err := syntax.ParseSource("factory.ub", src)
	require.NoError(t, err)
	require.NotNil(t, sf.Factory)
	return Input{
		FactoryBody: sf.Factory.Body,
		FactorySource: syntax.SourceFileSpec{
			DisplayPath:    "factory.ub",
			ProjectRelPath: "factory.ub",
			LineStarts:     parse.LineStarts(src),
		},
		FactoryName: factoryName,
		GoImports: map[string]string{
			"core": "example.com/core",
		},
	}
}

func testASTFactorySource() string {
	var b strings.Builder
	b.WriteString(testASTHeader("factory"))
	b.WriteString(" {\n")
	b.WriteString("  ")
	b.WriteString(testASTHeader("imports"))
	b.WriteString(" { core: 'example.com/core' }\n\n")
	b.WriteString("  ")
	b.WriteString(testASTHeader("actions"))
	b.WriteString(" { say: core.echo { echo: 'x' } }\n\n")
	b.WriteString("  ")
	b.WriteString(testASTHeader("outputs"))
	b.WriteString(" { message: { value: action.say.echo } }\n")
	b.WriteString("}\n")
	return b.String()
}

func testASTHeader(name string) string {
	return name + ":"
}
