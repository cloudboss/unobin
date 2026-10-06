package codegen

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestSyntaxEmissionImports(t *testing.T) {
	for _, test := range []struct {
		name string
		body syntax.FactoryBody
		lang bool
	}{
		{name: "empty"},
		{name: "address text", body: syntax.FactoryBody{
			StateMoves: []syntax.StateMoveDecl{{
				From: &syntax.StateMoveRef{Ref: runtime.EntryRef{Address: "resource.lang.old"}},
				To:   &syntax.StateMoveRef{Ref: runtime.EntryRef{Address: "resource.lang.new"}},
			}},
		}},
		{name: "expression", body: syntax.FactoryBody{
			Description: &lang.StringLit{Value: "example"},
		}, lang: true},
		{name: "input type", body: syntax.FactoryBody{
			Inputs: []syntax.InputDecl{{
				Name: syntax.Ident{Name: "value"}, Type: &lang.TypeAtomic{Name: "string"},
			}},
		}, lang: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			out, err := emitSyntaxFactoryBody(test.body, nil)
			require.NoError(t, err)
			require.Equal(t, test.lang, out.UsesLang)
			legacy, err := EncodeSyntaxFactoryBody(test.body)
			require.NoError(t, err)
			require.Equal(t, legacy, out.Literal)
		})
	}
}
