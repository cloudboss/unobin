package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSourceLowersFile(t *testing.T) {
	got, err := ParseSource("factory.ub", []byte(`
factory: {
  description: 'Example.'
}
`))
	require.NoError(t, err)
	require.Equal(t, FileFactory, got.Kind)
	require.NotNil(t, got.Factory)
	require.NotNil(t, got.Factory.Body.Description)
	assert.Equal(t, "Example.", got.Factory.Body.Description.Value)
}

func TestParseSourceReturnsLoweringDiagnostics(t *testing.T) {
	got, err := ParseSource("factory.ub", []byte("stack: {}\n"))

	require.Error(t, err)
	require.NotNil(t, got)
	assert.Contains(t, err.Error(), "factory.ub must declare factory")
}

func TestParseSourceReturnsParseError(t *testing.T) {
	got, err := ParseSource("factory.ub", []byte("factory: {\n"))

	require.Error(t, err)
	require.Nil(t, got)
}
