package lang

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateInputsRequiresLibraryConfigResolver(t *testing.T) {
	src, err := os.ReadFile("testdata/ub/inputs/valid/library-config-value.ub")
	require.NoError(t, err)
	decl := parseInputsBlock(t, string(src))
	declErrs := ValidateInputDeclarations(decl)
	require.Equal(t, 0, declErrs.Len(), declErrs.Error())

	_, errs := ValidateInputs(decl, map[string]any{
		"aws-config": map[string]any{"region": "us-east-1"},
	}, nil)

	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), `library-config "github.com/acme/aws"`)
}
