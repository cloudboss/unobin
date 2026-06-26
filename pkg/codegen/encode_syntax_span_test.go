package codegen

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func TestEncodeSyntaxFactoryBodyWithSpans(t *testing.T) {
	src := []byte(spanEncodingFactorySource())
	sf, err := syntax.ParseSource("factory.ub", src)
	require.NoError(t, err)
	require.NotNil(t, sf.Factory)

	expr, err := EncodeSyntaxFactoryBodyWithSpans(sf.Factory.Body, func(s parse.Span) string {
		return fmt.Sprintf("sp(%d, %d)", s.Start.Offset, s.End.Offset)
	})
	require.NoError(t, err)

	require.Contains(t, expr, "syntax.FactoryBody{S: sp(")
	require.Contains(t, expr, "Name: syntax.Ident{S: sp(")
	require.Contains(t, expr, "Body: &lang.ObjectLit{S: sp(")
	require.NotContains(t, expr, "parse.Position{")

	generated := `package generated

import (
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

var factorySource = parse.NewSourceFile("factory.ub", []int{0})

func sp(start, end int) parse.Span {
	return factorySource.Span(start, end)
}

var _ = ` + expr + `
var _ lang.Expr
`
	_, err = goparser.ParseFile(token.NewFileSet(), "generated.go", generated, 0)
	require.NoError(t, err)
}

func spanEncodingFactorySource() string {
	var b strings.Builder
	b.WriteString(spanEncodingHeader("factory"))
	b.WriteString(" {\n")
	b.WriteString("  ")
	b.WriteString(spanEncodingHeader("imports"))
	b.WriteString(" { core: 'example.com/core' }\n\n")
	b.WriteString("  ")
	b.WriteString(spanEncodingHeader("inputs"))
	b.WriteString(" { message: { type: string } }\n\n")
	b.WriteString("  ")
	b.WriteString(spanEncodingHeader("actions"))
	b.WriteString(" { say: core.echo { echo: 'x' } }\n\n")
	b.WriteString("  ")
	b.WriteString(spanEncodingHeader("outputs"))
	b.WriteString(" { message: { value: action.say.echo } }\n")
	b.WriteString("}\n")
	return b.String()
}

func spanEncodingHeader(name string) string {
	return name + ":"
}
