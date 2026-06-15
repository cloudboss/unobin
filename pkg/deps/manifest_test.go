package deps

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/ubtest"
)

func manifestFS(src string) fstest.MapFS {
	wrapped := "manifest: {\n" + src + "}\n"
	return fstest.MapFS{ManifestFileName: &fstest.MapFile{Data: []byte(wrapped)}}
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

func TestReadManifestFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/manifest", func(name string, src []byte) (string, []string) {
		m, err := ReadManifest(fstest.MapFS{
			ManifestFileName: &fstest.MapFile{Data: src},
		})
		if err != nil {
			return "", []string{err.Error()}
		}
		out, err := lang.Canonicalize(ManifestFileName, EncodeManifest(m))
		if err != nil {
			return "", []string{err.Error()}
		}
		return string(out), nil
	})
}

func TestReadManifestRejectsBadToolchainVersion(t *testing.T) {
	_, err := ReadManifest(manifestFS("unobin-version: 'latest'\nrequires: {}\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), `"latest" is not a valid version`)
}

func TestReadManifestRejectsNonStringToolchainLine(t *testing.T) {
	_, err := ReadManifest(manifestFS("unobin-version: {}\nrequires: {}\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unobin-version must be a string literal")
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

// TestEncodeManifestRoundTrips pins the encoder as stable: its output
// parses, reading it back recovers the manifest, and re-encoding is
// byte-identical.
func TestEncodeManifestRoundTrips(t *testing.T) {
	m := &Manifest{
		UnobinVersion: "v0.2.0",
		Requires: map[Dependency]string{
			{URL: "github.com/cloudboss/unobin-library-aws"}:         "v1.2.0",
			{URL: "github.com/cloudboss/unobin-library-std"}:         "v0.4.1",
			{URL: "github.com/example/mono", Subdir: "libs/network"}: "v2.0.0",
		},
		Replace: map[Dependency]string{
			{URL: "github.com/cloudboss/unobin-library-std"}: "../local-std",
		},
	}
	encoded := EncodeManifest(m)

	back, err := ReadManifest(fstest.MapFS{
		ManifestFileName: &fstest.MapFile{Data: encoded},
	})
	require.NoError(t, err)
	require.Equal(t, m.UnobinVersion, back.UnobinVersion)
	require.Equal(t, m.Requires, back.Requires)
	require.Equal(t, m.Replace, back.Replace)
	require.Equal(t, string(encoded), string(EncodeManifest(back)),
		"re-encoding a read-back manifest should be byte-stable")
}

func TestEncodeManifestWritesToolchainLine(t *testing.T) {
	m := &Manifest{
		UnobinVersion: "v0.2.0",
		Requires:      map[Dependency]string{},
	}
	encoded := EncodeManifest(m)
	require.Contains(t, string(encoded), "unobin-version: 'v0.2.0'\n")

	back, err := ReadManifest(fstest.MapFS{
		ManifestFileName: &fstest.MapFile{Data: encoded},
	})
	require.NoError(t, err)
	require.Equal(t, m.UnobinVersion, back.UnobinVersion)
}
