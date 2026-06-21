package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseForTest(t *testing.T, body string) string {
	t.Helper()
	return writeConfig(t, body)
}

func TestLoadParallelismNilFile(t *testing.T) {
	got, err := loadParallelism(nil, "")
	require.NoError(t, err)
	assert.Equal(t, 0, got)
}

func TestLoadParallelismAbsentField(t *testing.T) {
	path := parseForTest(t, `factory: { inputs: { region: 'us-east-1' } }`)
	f, err := parseStackFile(path)
	require.NoError(t, err)
	got, err := loadParallelism(f, path)
	require.NoError(t, err)
	assert.Equal(t, 0, got)
}

func TestLoadParallelismValid(t *testing.T) {
	path := parseForTest(t, `parallelism: 5
factory: { inputs: { region: 'us-east-1' } }
`)
	f, err := parseStackFile(path)
	require.NoError(t, err)
	got, err := loadParallelism(f, path)
	require.NoError(t, err)
	assert.Equal(t, 5, got)
}

