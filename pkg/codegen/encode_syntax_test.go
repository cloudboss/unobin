package codegen

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/stretchr/testify/require"
)

func TestEncodeSyntaxFactoryBodyIncludesStateMoves(t *testing.T) {
	body := syntax.FactoryBody{
		StateMoves: []syntax.StateMoveDecl{
			{
				From: &parse.StringLit{Value: "core.thing@resource.old"},
				To:   &parse.StringLit{Value: "core.thing@resource.new"},
			},
		},
	}

	got, err := EncodeSyntaxFactoryBody(body)

	require.NoError(t, err)
	assertion := "syntax.FactoryBody{" +
		"StateMoves: []syntax.StateMoveDecl{{" +
		`From: &lang.StringLit{Value: "core.thing@resource.old"}, ` +
		`To: &lang.StringLit{Value: "core.thing@resource.new"}` +
		"}}}"
	require.Equal(t, assertion, got)
}
