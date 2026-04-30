package resolve

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func TestLocalResolverRelative(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "modules", "net", "module.ub"),
		"description: 'net'\n")
	writeFile(t, filepath.Join(root, "modules", "net", "cluster.ub"),
		"description: 'cluster'\n")

	src, err := NewLocalResolver(root).Resolve(&LocalImport{Path: "./modules/net"})
	require.NoError(t, err)
	require.NotNil(t, src)
	require.Empty(t, src.Commit)
	require.Empty(t, src.Hash)

	b, err := fs.ReadFile(src.FS, "module.ub")
	require.NoError(t, err)
	require.Contains(t, string(b), "net")
}

func TestLocalResolverAbsolute(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "abs-module")
	writeFile(t, filepath.Join(target, "module.ub"), "description: 'abs'\n")

	src, err := NewLocalResolver(root).Resolve(&LocalImport{Path: target})
	require.NoError(t, err)
	require.NotNil(t, src)
}

func TestLocalResolverMissing(t *testing.T) {
	root := t.TempDir()
	_, err := NewLocalResolver(root).Resolve(&LocalImport{Path: "./does-not-exist"})
	require.Error(t, err)
	require.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestLocalResolverFileNotDir(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stray.ub"), "description: 'x'\n")
	_, err := NewLocalResolver(root).Resolve(&LocalImport{Path: "./stray.ub"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a directory")
}

func TestLocalResolverRejectsRemoteRef(t *testing.T) {
	_, err := NewLocalResolver("").Resolve(&RemoteImport{
		URL: "github.com/x/y", Version: "v1",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot handle")
}

func TestRemoteResolverStubErrors(t *testing.T) {
	_, err := NewRemoteResolver().Resolve(&RemoteImport{
		URL: "github.com/x/y", Version: "v1",
	})
	require.True(t, errors.Is(err, ErrRemoteNotImplemented))
}

func TestRemoteResolverRejectsLocalRef(t *testing.T) {
	_, err := NewRemoteResolver().Resolve(&LocalImport{Path: "./x"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot handle")
}

func TestIsUBModule(t *testing.T) {
	root := t.TempDir()
	withManifest := filepath.Join(root, "with")
	writeFile(t, filepath.Join(withManifest, "module.ub"), "description: 'x'\n")
	without := filepath.Join(root, "without")
	require.NoError(t, os.MkdirAll(without, 0o755))

	r := NewLocalResolver(root)
	yes, err := r.Resolve(&LocalImport{Path: "./with"})
	require.NoError(t, err)
	require.True(t, IsUBModule(yes))

	no, err := r.Resolve(&LocalImport{Path: "./without"})
	require.NoError(t, err)
	require.False(t, IsUBModule(no))
}

func TestIsUBModuleNilSafe(t *testing.T) {
	require.False(t, IsUBModule(nil))
	require.False(t, IsUBModule(&Source{}))
}
