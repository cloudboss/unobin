package deps

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func manifestFS(src string) fstest.MapFS {
	return fstest.MapFS{ManifestFileName: &fstest.MapFile{Data: []byte(src)}}
}

func TestReadManifestToolchainLine(t *testing.T) {
	m, err := ReadManifest(manifestFS("unobin-version: 'v0.2.0'\nrequires: {}\n"))
	require.NoError(t, err)
	require.Equal(t, "v0.2.0", m.UnobinVersion)
}

func TestReadManifestWithoutToolchainLine(t *testing.T) {
	m, err := ReadManifest(manifestFS("requires: {}\n"))
	require.NoError(t, err)
	require.Empty(t, m.UnobinVersion)
}

func TestReadManifestRejectsBadToolchainVersion(t *testing.T) {
	_, err := ReadManifest(manifestFS("unobin-version: 'latest'\nrequires: {}\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), `"latest" is not a valid version`)
}

func TestReadManifestRejectsNonStringToolchainLine(t *testing.T) {
	_, err := ReadManifest(manifestFS("unobin-version: {}\nrequires: {}\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "version string")
}

// TestReadManifestRejectsUnobinInRequires proves the unobin repo
// cannot be a floored dependency: its version is the toolchain's to
// pin, through the manifest's unobin-version line.
func TestReadManifestRejectsUnobinInRequires(t *testing.T) {
	_, err := ReadManifest(manifestFS(
		"requires: {\n  'github.com/cloudboss/unobin': 'v0.5.0'\n}\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolchain-versioned")
	require.Contains(t, err.Error(), "unobin-version line")
}

func TestEncodeManifestWritesToolchainLine(t *testing.T) {
	m := &Manifest{
		UnobinVersion: "v0.2.0",
		Requires:      map[Dependency]string{},
	}
	encoded := EncodeManifest(m)
	require.Contains(t, string(encoded), "unobin-version: 'v0.2.0'\n")

	back, err := ReadManifest(manifestFS(string(encoded)))
	require.NoError(t, err)
	require.Equal(t, m.UnobinVersion, back.UnobinVersion)
}
