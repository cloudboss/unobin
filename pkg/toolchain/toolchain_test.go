package toolchain

import (
	"runtime"
	"testing"

	"github.com/cloudboss/cachedeps"
	"github.com/stretchr/testify/assert"
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
