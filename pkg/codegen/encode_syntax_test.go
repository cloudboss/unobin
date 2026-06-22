package codegen

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/stateref"
	"github.com/stretchr/testify/require"
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
