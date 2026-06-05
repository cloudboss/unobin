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
	m, err := ReadManifest(manifestFS("unobin: 'v0.2.0'\nrequires: {}\n"))
	require.NoError(t, err)
	require.Equal(t, "v0.2.0", m.UnobinVersion)
}

func TestReadManifestWithoutToolchainLine(t *testing.T) {
	m, err := ReadManifest(manifestFS("requires: {}\n"))
	require.NoError(t, err)
	require.Empty(t, m.UnobinVersion)
}

func TestReadManifestRejectsBadToolchainVersion(t *testing.T) {
	_, err := ReadManifest(manifestFS("unobin: 'latest'\nrequires: {}\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), `"latest" is not a valid version`)
}

func TestReadManifestRejectsNonStringToolchainLine(t *testing.T) {
	_, err := ReadManifest(manifestFS("unobin: {}\nrequires: {}\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "version string")
}

func TestEncodeManifestWritesToolchainLine(t *testing.T) {
	m := &Manifest{
		UnobinVersion: "v0.2.0",
		Requires:      map[Dependency]string{},
	}
	encoded := EncodeManifest(m)
	require.Contains(t, string(encoded), "unobin: 'v0.2.0'\n")

	back, err := ReadManifest(manifestFS(string(encoded)))
	require.NoError(t, err)
	require.Equal(t, m.UnobinVersion, back.UnobinVersion)
}
