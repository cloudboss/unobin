package compile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/toolchain"
)

func noDownload(t *testing.T) func(string) error {
	t.Helper()
	return func(string) error {
		t.Fatal("download must not run")
		return nil
	}
}

func noSourceRoot() (string, bool) {
	return "", false
}

func sourceRoot(dir string) func() (string, bool) {
	return func() (string, bool) {
		return dir, true
	}
}

func cachedUnobinDir(t *testing.T, cache, version string) string {
	t.Helper()
	dir := filepath.Join(cache, "github.com", "cloudboss", "unobin@"+version)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

func TestUnobinModuleRootPrefersReplace(t *testing.T) {
	root, ok := unobinModuleRoot("/src/unobin", "v1.2.3", noSourceRoot, noDownload(t))
	require.True(t, ok)
	require.Equal(t, toolchain.UnobinModulePath, root.Path)
	require.Equal(t, "/src/unobin", root.Dir)
}

func TestUnobinModuleRootUsesSourceRoot(t *testing.T) {
	root, ok := unobinModuleRoot("", "dev", sourceRoot("/src/unobin"), noDownload(t))
	require.True(t, ok)
	require.Equal(t, toolchain.UnobinModulePath, root.Path)
	require.Equal(t, "/src/unobin", root.Dir)
}

func TestUnobinModuleRootRefusesDevWithoutSource(t *testing.T) {
	_, ok := unobinModuleRoot("", "dev", noSourceRoot, noDownload(t))
	require.False(t, ok)
}

func TestUnobinModuleRootFindsCachedModule(t *testing.T) {
	cache := t.TempDir()
	dir := cachedUnobinDir(t, cache, "v1.2.3")
	t.Setenv("GOMODCACHE", cache)
	root, ok := unobinModuleRoot("", "v1.2.3", noSourceRoot, noDownload(t))
	require.True(t, ok)
	require.Equal(t, toolchain.UnobinModulePath, root.Path)
	require.Equal(t, dir, root.Dir)
}

func TestUnobinModuleRootPrefersCacheForPinnedVersion(t *testing.T) {
	cache := t.TempDir()
	dir := cachedUnobinDir(t, cache, "v1.2.3")
	t.Setenv("GOMODCACHE", cache)
	root, ok := unobinModuleRoot("", "v1.2.3", sourceRoot("/src/unobin"), noDownload(t))
	require.True(t, ok)
	require.Equal(t, toolchain.UnobinModulePath, root.Path)
	require.Equal(t, dir, root.Dir)
}

func TestUnobinModuleRootFindsCachedDirtyModule(t *testing.T) {
	cache := t.TempDir()
	dir := cachedUnobinDir(t, cache, "v1.2.3")
	t.Setenv("GOMODCACHE", cache)
	root, ok := unobinModuleRoot("", "v1.2.3+dirty", noSourceRoot, noDownload(t))
	require.True(t, ok)
	require.Equal(t, toolchain.UnobinModulePath, root.Path)
	require.Equal(t, dir, root.Dir)
}

func TestUnobinModuleRootDownloadsOnCacheMiss(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("GOMODCACHE", cache)
	root, ok := unobinModuleRoot("", "v1.2.3", noSourceRoot, func(version string) error {
		require.Equal(t, "v1.2.3", version)
		cachedUnobinDir(t, cache, version)
		return nil
	})
	require.True(t, ok)
	require.Equal(t, filepath.Join(cache, "github.com", "cloudboss", "unobin@v1.2.3"), root.Dir)
}

func TestUnobinSourceRootFromFileFindsModule(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte(
		"module "+toolchain.UnobinModulePath+"\n"), 0o644))
	subdir := filepath.Join(root, "pkg", "compile")
	require.NoError(t, os.MkdirAll(subdir, 0o755))

	got, ok := unobinSourceRootFromFile(filepath.Join(subdir, "unobinroot.go"))

	require.True(t, ok)
	require.Equal(t, root, got)
}

func TestUnobinModuleRootDegradesWhenDownloadFails(t *testing.T) {
	t.Setenv("GOMODCACHE", t.TempDir())
	_, ok := unobinModuleRoot("", "v1.2.3", noSourceRoot, func(string) error {
		return errors.New("offline")
	})
	require.False(t, ok)
}

func TestGoModCacheDirPrecedence(t *testing.T) {
	t.Setenv("GOMODCACHE", "/explicit/cache")
	require.Equal(t, "/explicit/cache", goModCacheDir())

	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "/first"+string(os.PathListSeparator)+"/second")
	require.Equal(t, filepath.Join("/first", "pkg", "mod"), goModCacheDir())

	t.Setenv("GOPATH", "")
	t.Setenv("HOME", "/home/someone")
	require.Equal(t, filepath.Join("/home/someone", "go", "pkg", "mod"), goModCacheDir())
}
