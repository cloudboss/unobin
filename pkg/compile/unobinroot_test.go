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

func cachedUnobinDir(t *testing.T, cache, version string) string {
	t.Helper()
	dir := filepath.Join(cache, "github.com", "cloudboss", "unobin@"+version)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

func TestUnobinModuleRootPrefersReplace(t *testing.T) {
	root, ok := unobinModuleRoot("/src/unobin", "v1.2.3", noDownload(t))
	require.True(t, ok)
	require.Equal(t, toolchain.UnobinModulePath, root.Path)
	require.Equal(t, "/src/unobin", root.Dir)
}

func TestUnobinModuleRootRefusesDevWithoutReplace(t *testing.T) {
	_, ok := unobinModuleRoot("", "dev", noDownload(t))
	require.False(t, ok)
}

func TestUnobinModuleRootFindsCachedModule(t *testing.T) {
	cache := t.TempDir()
	dir := cachedUnobinDir(t, cache, "v1.2.3")
	t.Setenv("GOMODCACHE", cache)
	root, ok := unobinModuleRoot("", "v1.2.3", noDownload(t))
	require.True(t, ok)
	require.Equal(t, toolchain.UnobinModulePath, root.Path)
	require.Equal(t, dir, root.Dir)
}

func TestUnobinModuleRootDownloadsOnCacheMiss(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("GOMODCACHE", cache)
	root, ok := unobinModuleRoot("", "v1.2.3", func(version string) error {
		require.Equal(t, "v1.2.3", version)
		cachedUnobinDir(t, cache, version)
		return nil
	})
	require.True(t, ok)
	require.Equal(t, filepath.Join(cache, "github.com", "cloudboss", "unobin@v1.2.3"), root.Dir)
}

func TestUnobinModuleRootDegradesWhenDownloadFails(t *testing.T) {
	t.Setenv("GOMODCACHE", t.TempDir())
	_, ok := unobinModuleRoot("", "v1.2.3", func(string) error {
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
