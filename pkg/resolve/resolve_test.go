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
	writeFile(t, filepath.Join(root, "libraries", "net", "library.ub"),
		"description: 'net'\n")
	writeFile(t, filepath.Join(root, "libraries", "net", "cluster.ub"),
		"description: 'cluster'\n")

	src, err := NewLocalResolver(root).Resolve(&LocalImport{Path: "./libraries/net"})
	require.NoError(t, err)
	require.NotNil(t, src)
	require.Empty(t, src.Commit)

	b, err := fs.ReadFile(src.FS, "library.ub")
	require.NoError(t, err)
	require.Contains(t, string(b), "net")
}

func TestLocalResolverAbsolute(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "abs-library")
	writeFile(t, filepath.Join(target, "library.ub"), "description: 'abs'\n")

	_, err := NewLocalResolver(root).Resolve(&LocalImport{Path: target})
	require.Error(t, err)
	require.Contains(t, err.Error(), "absolute")
}

func TestLocalResolverMissing(t *testing.T) {
	root := t.TempDir()
	_, err := NewLocalResolver(root).Resolve(&LocalImport{Path: "./does-not-exist"})
	require.Error(t, err)
	require.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestLocalResolverRejectsSymlinkPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real-library")
	writeFile(t, filepath.Join(target, "library.ub"), "description: 'real'\n")
	require.NoError(t, os.Symlink(target, filepath.Join(root, "link-library")))

	_, err := NewLocalResolver(root).Resolve(&LocalImport{Path: "./link-library"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "symlink")
}

func TestLocalResolverRejectsImportOutsideProjectRoot(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "project")
	writeFile(t, filepath.Join(project, "manifest.ub"), "manifest: { requires: {} }\n")
	writeFile(t, filepath.Join(base, "shared", "library.ub"), "thing: resource {}\n")

	_, err := NewLocalResolver(project).Resolve(&LocalImport{Path: "../shared"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside project root")
}

func TestLocalResolverRejectsImportIntoNestedManifestRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "manifest.ub"), "manifest: { requires: {} }\n")
	writeFile(t, filepath.Join(root, "shared", "abc", "manifest.ub"),
		"manifest: { requires: {} }\n")
	writeFile(t, filepath.Join(root, "shared", "abc", "library.ub"), "thing: resource {}\n")

	_, err := NewLocalResolver(root).Resolve(&LocalImport{Path: "./shared/abc"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nested project")
}

func TestLocalResolverUnmarkedRootDoesNotClassifyTargetProject(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "shared", "manifest.ub"), "manifest: not-valid\n")
	writeFile(t, filepath.Join(root, "shared", "library.ub"), "thing: resource {}\n")

	_, err := NewLocalResolver(root).Resolve(&LocalImport{Path: "./shared"})
	require.NoError(t, err)
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

func TestRemoteResolverRejectsLocalRef(t *testing.T) {
	r, err := NewRemoteResolver()
	require.NoError(t, err)
	_, err = r.Resolve(&LocalImport{Path: "./x"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot handle")
}

func TestIsUBLibrary(t *testing.T) {
	root := t.TempDir()
	withManifest := filepath.Join(root, "with")
	writeFile(t, filepath.Join(withManifest, "library.ub"),
		"thing: resource { description: 'x' }\n")
	without := filepath.Join(root, "without")
	require.NoError(t, os.MkdirAll(without, 0o755))

	r := NewLocalResolver(root)
	yes, err := r.Resolve(&LocalImport{Path: "./with"})
	require.NoError(t, err)
	require.True(t, IsUBLibrary(yes))

	no, err := r.Resolve(&LocalImport{Path: "./without"})
	require.NoError(t, err)
	require.False(t, IsUBLibrary(no))
}

func TestIsUBLibraryNilSafe(t *testing.T) {
	require.False(t, IsUBLibrary(nil))
	require.False(t, IsUBLibrary(&Source{}))
}
