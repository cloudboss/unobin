package codegen

import (
	goparser "go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/stateref"
)

func TestEncodeSyntaxFactoryBodyIncludesStateMoves(t *testing.T) {
	body := syntax.FactoryBody{
		StateMoves: []syntax.StateMoveDecl{
			{
				From: &syntax.StateMoveRef{Ref: stateref.EntryRef{Address: "resource.old"}},
				To:   &syntax.StateMoveRef{Ref: stateref.EntryRef{Address: "resource.new"}},
			},
		},
	}

	got, err := EncodeSyntaxFactoryBody(body)

	require.NoError(t, err)
	assertion := "syntax.FactoryBody{" +
		"StateMoves: []syntax.StateMoveDecl{{" +
		`From: &syntax.StateMoveRef{Ref: runtime.EntryRef{Address: "resource.old"}}, ` +
		`To: &syntax.StateMoveRef{Ref: runtime.EntryRef{Address: "resource.new"}}` +
		"}}}"
	require.Equal(t, assertion, got)
}

func TestEncodeSyntaxFactoryBodyIncludesAssets(t *testing.T) {
	src := ubtest.ReadValidFixture(t, "testdata/ub/encode-syntax", "assets")
	sf, err := syntax.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)
	require.NotNil(t, sf.Factory)

	got, err := EncodeSyntaxFactoryBodyWithSpans(sf.Factory.Body, func(s parse.Span) string {
		return "span"
	})

	require.NoError(t, err)
	assert.Contains(t, got, "Assets: []syntax.AssetDecl{")
	assert.Contains(t, got, `Name: syntax.Ident{S: span, Name: "lambda"}`)
	assert.Contains(t, got, `Source: &lang.StringLit{S: span, Value: "./lambda"}`)
	assert.Contains(t, got, `Name: syntax.Ident{S: span, Name: "archive"}`)
	assert.Contains(t, got, `Source: &lang.StringLit{S: span, Value: "./build/lambda.zip"}`)

	generated := "package generated\n\n" +
		"import (\n" +
		"\t\"github.com/cloudboss/unobin/pkg/lang\"\n" +
		"\t\"github.com/cloudboss/unobin/pkg/lang/parse\"\n" +
		"\t\"github.com/cloudboss/unobin/pkg/lang/syntax\"\n" +
		")\n\n" +
		"var span = parse.Span{}\n" +
		"var _ = " + got + "\n"
	_, err = goparser.ParseFile(token.NewFileSet(), "generated.go", generated, 0)
	require.NoError(t, err)
}
