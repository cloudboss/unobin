package deps

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/ubtest"
)

func manifestFixtureFS(t testing.TB, path string) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		ManifestFileName: &fstest.MapFile{Data: []byte(ubtest.ReadFixture(t, path))},
	}
}

func TestReadManifestToolchainLine(t *testing.T) {
	m, err := ReadManifest(manifestFixtureFS(t, "testdata/ub/manifest/valid/basic.ub"))
	require.NoError(t, err)
	require.Equal(t, "v0.2.0", m.UnobinVersion)
}

func TestReadManifestWithoutToolchainLine(t *testing.T) {
	m, err := ReadManifest(manifestFixtureFS(t, "testdata/ub/manifest/valid/empty.ub"))
	require.NoError(t, err)
	require.Empty(t, m.UnobinVersion)
}

func TestReadManifestObjectRequirements(t *testing.T) {
	m, err := ReadManifest(
		manifestFixtureFS(t, "testdata/ub/manifest/valid/object-requirements.ub"))
	require.NoError(t, err)
	require.Equal(t, map[Dependency]Requirement{
		{URL: "github.com/x/direct"}:   {Version: "v1.2.3"},
		{URL: "github.com/x/indirect"}: {Version: "v2.0.0", Indirect: true},
	}, m.Requires)
	require.Equal(t, map[Dependency]string{
		{URL: "github.com/x/direct"}:   "v1.2.3",
		{URL: "github.com/x/indirect"}: "v2.0.0",
	}, m.RequireVersions())
	require.Equal(t, map[Dependency]string{{URL: "github.com/x/direct"}: "v1.2.3"},
		m.DirectRequireVersions())
	require.Equal(t, 1, m.DirectCount())
	require.Equal(t, 1, m.IndirectCount())
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
	_, err := ReadManifest(
		manifestFixtureFS(t, "testdata/ub/manifest/invalid/bad-toolchain-version.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), `"latest" is not a valid version`)
}

func TestReadManifestRejectsNonStringToolchainLine(t *testing.T) {
	_, err := ReadManifest(
		manifestFixtureFS(t, "testdata/ub/manifest/invalid/non-string-toolchain.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unobin-version must be a string literal")
}

// TestReadManifestRejectsUnobinInRequires proves the unobin repo
// cannot be a floored dependency: its version is the toolchain's to
// pin, through the manifest's unobin-version line.
func TestReadManifestRejectsUnobinInRequires(t *testing.T) {
	_, err := ReadManifest(manifestFixtureFS(t, "testdata/ub/manifest/invalid/unobin-required.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolchain-versioned")
	require.Contains(t, err.Error(), "unobin-version line")
}

// TestEncodeManifestCanBeReadAgain pins the encoder as stable: its output
// parses, reading it back recovers the manifest, and re-encoding is
// byte-identical.
func TestEncodeManifestCanBeReadAgain(t *testing.T) {
	m := &Manifest{
		UnobinVersion: "v0.2.0",
		Requires: map[Dependency]Requirement{
			{URL: "github.com/cloudboss/unobin-library-aws"}: {Version: "v1.2.0"},
			{URL: "github.com/cloudboss/unobin-library-std"}: {
				Version:  "v0.4.1",
				Indirect: true,
			},
			{URL: "github.com/example/mono", Subdir: "libs/network"}: {Version: "v2.0.0"},
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
		Requires:      map[Dependency]Requirement{},
	}
	encoded := EncodeManifest(m)
	require.Contains(t, string(encoded), "unobin-version: 'v0.2.0'\n")

	back, err := ReadManifest(fstest.MapFS{
		ManifestFileName: &fstest.MapFile{Data: encoded},
	})
	require.NoError(t, err)
	require.Equal(t, m.UnobinVersion, back.UnobinVersion)
}
