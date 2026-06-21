package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseParallelismFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", "ub", "parallelism", "valid", name+".ub")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return writeConfig(t, string(body))
}

func TestLoadParallelismNilFile(t *testing.T) {
	got, err := loadParallelism(nil, "")
	require.NoError(t, err)
	assert.Equal(t, 0, got)
}

func TestLoadParallelismAbsentField(t *testing.T) {
	path := parseParallelismFixture(t, "absent")
	f, err := parseStackFile(path)
	require.NoError(t, err)
	got, err := loadParallelism(f, path)
	require.NoError(t, err)
	assert.Equal(t, 0, got)
}

func TestLoadParallelismValid(t *testing.T) {
	path := parseParallelismFixture(t, "value")
	f, err := parseStackFile(path)
	require.NoError(t, err)
	got, err := loadParallelism(f, path)
	require.NoError(t, err)
	assert.Equal(t, 5, got)
}
