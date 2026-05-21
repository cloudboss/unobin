package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func parseOutputsBlock(t *testing.T, src string) *ObjectLit {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	require.NotEmpty(t, f.Body.Fields)
	require.Equal(t, "outputs", f.Body.Fields[0].Key.Name)
	o, ok := f.Body.Fields[0].Value.(*ObjectLit)
	require.True(t, ok, "expected `outputs:` to be an object literal")
	return o
}

func TestSensitiveInputs(t *testing.T) {
	src := `
inputs: {
  region: { type: string }
  password: {
    type: string
    @sensitive: true
  }
  token: {
    type: string
    @sensitive: false
  }
  api-key: {
    type: string
    @sensitive: true
  }
}
`
	got := SensitiveInputs(parseInputsBlock(t, src))
	require.Equal(t, map[string]bool{"password": true, "api-key": true}, got)
}

func TestSensitiveInputsNilBlock(t *testing.T) {
	require.Empty(t, SensitiveInputs(nil))
}

func TestSensitiveOutputs(t *testing.T) {
	src := `
outputs: {
  url: { value: 'https://example.com' }
  password: {
    value: var.p
    @sensitive: true
  }
  token: {
    value: 'cleartext'
    @sensitive: false
  }
}
`
	got := SensitiveOutputs(parseOutputsBlock(t, src))
	require.Equal(t, map[string]bool{"password": true}, got)
}

func TestSensitiveOutputsNilBlock(t *testing.T) {
	require.Empty(t, SensitiveOutputs(nil))
}
