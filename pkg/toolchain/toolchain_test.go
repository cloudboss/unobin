package toolchain

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/cloudboss/cachedeps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryHasCurrentPlatform(t *testing.T) {
	platform := cachedeps.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	for _, dep := range All {
		_, ok := dep.URLs[platform]
		assert.True(t, ok, "%s missing URL for %s/%s",
			dep.Name, runtime.GOOS, runtime.GOARCH)
		_, ok = dep.SHA256[platform]
		assert.True(t, ok, "%s missing SHA256 for %s/%s",
			dep.Name, runtime.GOOS, runtime.GOARCH)
	}
}

func TestEnsureDependencyLocksConcurrentCalls(t *testing.T) {
	setTestCacheRoot(t)
	cache := cachedeps.New("unobin-test")
	dep := cachedeps.Dependency{Name: "go", Version: "concurrent"}
	const workers = 8
	var mu sync.Mutex
	active := 0
	maxActive := 0
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			<-start
			_, err := ensureDependency(cache, dep, func() (string, error) {
				mu.Lock()
				active++
				if active > maxActive {
					maxActive = active
				}
				current := active
				mu.Unlock()
				defer func() {
					mu.Lock()
					active--
					mu.Unlock()
				}()
				if current > 1 {
					return "", fmt.Errorf("%d concurrent cache fills", current)
				}
				time.Sleep(10 * time.Millisecond)
				return "cached", nil
			})
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, maxActive)
}

func TestEnsureDependencyReleasesLockAfterError(t *testing.T) {
	setTestCacheRoot(t)
	cache := cachedeps.New("unobin-test")
	dep := cachedeps.Dependency{Name: "go", Version: "error"}
	boom := errors.New("boom")

	_, err := ensureDependency(cache, dep, func() (string, error) {
		return "", boom
	})
	require.ErrorIs(t, err, boom)

	got, err := ensureDependency(cache, dep, func() (string, error) {
		return "cached", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "cached", got)
}

func setTestCacheRoot(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("LOCALAPPDATA", root)
}
