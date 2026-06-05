// Package toolchain pins the external tools `unobin compile` shells out
// to and fetches them through a shared cache, so builds use the same
// versions across machines regardless of what is on ${PATH}.
package toolchain

import (
	"io"
	"path/filepath"

	"github.com/cloudboss/cachedeps"
)

// UnobinModulePath is the unobin module's path: the one requirement
// every generated go.mod pins to the compiling CLI's version, and the
// dependency a factory checks its own build info for at startup.
const UnobinModulePath = "github.com/cloudboss/unobin"

// Go is the pinned Go toolchain `unobin compile` invokes when building
// factory binaries. Bumping the version means updating both the URL and
// the SHA256 for every supported platform.
var Go = cachedeps.Dependency{
	Name:    "go",
	Version: "1.26.2",
	Format:  cachedeps.TarGz,
	URLs: map[cachedeps.Platform]string{
		{OS: "linux", Arch: "amd64"}:  "https://go.dev/dl/go1.26.2.linux-amd64.tar.gz",
		{OS: "linux", Arch: "arm64"}:  "https://go.dev/dl/go1.26.2.linux-arm64.tar.gz",
		{OS: "darwin", Arch: "amd64"}: "https://go.dev/dl/go1.26.2.darwin-amd64.tar.gz",
		{OS: "darwin", Arch: "arm64"}: "https://go.dev/dl/go1.26.2.darwin-arm64.tar.gz",
	},
	SHA256: map[cachedeps.Platform]string{
		{OS: "linux", Arch: "amd64"}:  "990e6b4bbba816dc3ee129eaeaf4b42f17c2800b88a2166c265ac1a200262282",
		{OS: "linux", Arch: "arm64"}:  "c958a1fe1b361391db163a485e21f5f228142d6f8b584f6bef89b26f66dc5b23",
		{OS: "darwin", Arch: "amd64"}: "bc3f1500d9968c36d705442d90ba91addf9271665033748b82532682e90a7966",
		{OS: "darwin", Arch: "arm64"}: "32af1522bf3e3ff3975864780a429cc0b41d190ec7bf90faa661d6d64566e7af",
	},
}

// All is the set of pinned tools, used by tests to confirm coverage for
// the running platform.
var All = []cachedeps.Dependency{Go}

// Ensure returns the path to the pinned `go` executable, fetching and
// caching the toolchain under the user cache dir on first use. Progress
// is written to out; pass nil for silent operation. The archive unpacks
// to a top-level `go/` directory, so the executable sits at go/bin/go
// under the returned cache entry.
func Ensure(out io.Writer) (string, error) {
	cache := cachedeps.New("unobin")
	cache.Output = out
	dir, err := cache.Ensure(Go)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "go", "bin", "go"), nil
}
