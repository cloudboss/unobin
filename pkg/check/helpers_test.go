package check

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

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

// checkReferences runs the reference check the way production callers
// do, for the many tests that need only the diagnostics.
func checkReferences(f *lang.File, libs map[string]*runtime.Library) *lang.ErrorList {
	return New(f, libs).References(nil)
}

// checkLiteralConstraints mirrors checkReferences for the literal
// constraint check.
func checkLiteralConstraints(f *lang.File, libs map[string]*runtime.Library) *lang.ErrorList {
	return New(f, libs).LiteralConstraints()
}

func parseValue(t *testing.T, src string) lang.Expr {
	t.Helper()
	f, err := lang.ParseSource("", []byte("v: "+src+"\n"))
	require.NoError(t, err)
	require.Len(t, f.Body.Fields, 1)
	return f.Body.Fields[0].Value
}
