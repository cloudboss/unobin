package e2etest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareEmptyDirectories(t *testing.T) {
	workspace := t.TempDir()

	require.NoError(t, prepareEmptyDirectories(workspace, []string{"assets/tree/empty"}))
	require.DirExists(t, filepath.Join(workspace, "assets", "tree", "empty"))
}

func TestRemoveCasePaths(t *testing.T) {
	workspace := t.TempDir()
	removePath := filepath.Join(workspace, "assets", "tree")
	keepPath := filepath.Join(workspace, "keep.txt")
	require.NoError(t, os.MkdirAll(removePath, 0o755))
	writeText(t, filepath.Join(removePath, "file.txt"), "remove\n")
	writeText(t, keepPath, "keep\n")

	require.NoError(t, removeCasePaths(workspace, []string{"assets/tree"}))
	require.NoDirExists(t, removePath)
	require.FileExists(t, keepPath)

	err := removeCasePaths(workspace, []string{"missing"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing does not exist")

	err = removeCasePaths(workspace, []string{"."})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must name a path under the case directory")
	require.DirExists(t, workspace)
}

func TestCheckAssetBundleRejectsInvalidBundle(t *testing.T) {
	workspace := t.TempDir()
	buildDir := filepath.Join(workspace, ".e2e", "build")
	require.NoError(t, os.MkdirAll(buildDir, 0o755))
	writeText(t, filepath.Join(buildDir, "factory.assets"), "not a bundle")

	err := checkAssetBundle(workspace, &AssetBundleCheck{SetCount: 1, BlobCount: 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "open asset bundle")
}

func TestCheckAssetCache(t *testing.T) {
	workspace := t.TempDir()
	writeText(t, filepath.Join(workspace, "cache", "one", "complete"), "{}\n")
	writeText(t, filepath.Join(workspace, "cache", "two", "complete"), "{}\n")

	require.NoError(t, checkAssetCache(workspace, &AssetCacheCheck{
		Path:           "cache",
		ReferenceCount: 2,
	}))
	err := checkAssetCache(workspace, &AssetCacheCheck{
		Path:           "cache",
		ReferenceCount: 1,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset cache reference count is 2, want 1")
}
