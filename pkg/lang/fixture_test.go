package lang

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// loadFixture reads path as UB source and returns the parsed File. The
// path is taken relative to the test's working directory (pkg/lang).
// Fixtures shared with the parse subpackage are read via paths like
// `parse/testdata/valid/<name>.ub`.
func loadFixture(t *testing.T, path string) *File {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	f, err := ParseSource(path, b)
	require.NoError(t, err, "parse %s", path)
	return f
}
