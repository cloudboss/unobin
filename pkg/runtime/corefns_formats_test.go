package runtime

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func TestCoreFormatPredicates(t *testing.T) {
	src, err := os.ReadFile("testdata/ub/core-formats/valid/predicates.ub")
	require.NoError(t, err)
	file, err := lang.ParseSource("predicates.ub", src)
	require.NoError(t, err)
	require.Len(t, file.Body.Fields, 1)
	got, err := Eval(file.Body.Fields[0].Value, &EvalContext{})
	require.NoError(t, err)
	values, ok := got.(map[string]any)
	require.True(t, ok)
	for name, value := range values {
		require.Equal(t, true, value, name)
	}
}

func TestCoreFormatPredicateFaces(t *testing.T) {
	for _, name := range []string{
		"is-match", "is-format",
	} {
		t.Run(name, func(t *testing.T) {
			sig, ok := CoreFunctionSigs()[name]
			require.True(t, ok)
			for _, value := range []struct {
				value any
				typ   typecheck.Type
			}{
				{nil, typecheck.TNull()}, {"text", typecheck.TString()},
				{int64(1), typecheck.TInteger()}, {false, typecheck.TBoolean()},
				{[]any{}, typecheck.TList(typecheck.TString())},
				{map[string]any{}, typecheck.TMap(typecheck.TString())},
			} {
				args := []any{value.value}
				pattern := "^text$"
				if name == "is-format" {
					pattern = "regex"
				}
				args = append(args, pattern)
				_, err := coreFunctions[name].Func(args)
				require.Equal(t, typecheck.Assignable(sig.Params[0], value.typ), err == nil)
			}
		})
	}
}
