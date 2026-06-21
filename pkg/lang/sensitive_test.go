package lang

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func parseOutputsBlock(t *testing.T, src []byte) *ObjectLit {
	t.Helper()
	f, err := ParseSource("", src)
	require.NoError(t, err)
	require.NotEmpty(t, f.Body.Fields)
	require.Equal(t, "outputs", f.Body.Fields[0].Key.Name)
	o, ok := f.Body.Fields[0].Value.(*ObjectLit)
	require.True(t, ok, "expected `outputs:` to be an object literal")
	return o
}

func TestSensitiveInputs(t *testing.T) {
	src, err := os.ReadFile("testdata/ub/sensitive/valid/inputs.ub")
	require.NoError(t, err)

	got := SensitiveInputs(parseInputsBlock(t, string(src)))
	require.Equal(t, map[string]bool{"password": true, "api-key": true}, got)
}

func TestSensitiveInputsNilBlock(t *testing.T) {
	require.Empty(t, SensitiveInputs(nil))
}

func TestSensitiveOutputs(t *testing.T) {
	src, err := os.ReadFile("testdata/ub/sensitive/valid/outputs.ub")
	require.NoError(t, err)

	got := SensitiveOutputs(parseOutputsBlock(t, src))
	require.Equal(t, map[string]bool{"password": true}, got)
}

func TestSensitiveOutputsNilBlock(t *testing.T) {
	require.Empty(t, SensitiveOutputs(nil))
}
