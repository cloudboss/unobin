package syntax

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readParseFixture(t *testing.T, path string) []byte {
	t.Helper()
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	return src
}

func TestParseSourceLowersFile(t *testing.T) {
	got, err := ParseSource("factory.ub",
		readParseFixture(t, "testdata/ub/parse-source/valid/factory.ub"))
	require.NoError(t, err)
	require.Equal(t, FileFactory, got.Kind)
	require.NotNil(t, got.Factory)
	require.NotNil(t, got.Factory.Body.Description)
	assert.Equal(t, "Example.", got.Factory.Body.Description.Value)
}

func TestParseSourceReturnsLoweringDiagnostics(t *testing.T) {
	got, err := ParseSource("factory.ub",
		readParseFixture(t, "testdata/ub/parse-source/invalid/stack.ub"))

	require.Error(t, err)
	require.NotNil(t, got)
	assert.Contains(t, err.Error(), "factory.ub must declare factory")
}

func TestParseSourceReturnsParseError(t *testing.T) {
	got, err := ParseSource("factory.ub",
		readParseFixture(t, "testdata/ub/parse-source/invalid/open-factory.ub"))

	require.Error(t, err)
	require.Nil(t, got)
}
