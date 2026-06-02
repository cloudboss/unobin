package resolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanImports(t *testing.T) {
	root := t.TempDir()
	r := &RemoteResolver{CacheRoot: root}
	cached := filepath.Join(r.ImportsDir(), "github.com", "x", "y", "abc123")
	require.NoError(t, os.MkdirAll(cached, 0o755))

	dir, err := r.CleanImports()
	require.NoError(t, err)
	require.Equal(t, r.ImportsDir(), dir)
	_, statErr := os.Stat(r.ImportsDir())
	require.True(t, os.IsNotExist(statErr))
}

func TestCleanImportsNoCache(t *testing.T) {
	r := &RemoteResolver{CacheRoot: t.TempDir()}
	_, err := r.CleanImports()
	require.NoError(t, err)
}
