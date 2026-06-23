package deps

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
)

func projectFixtureFS(t testing.TB, path string) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		ProjectFileName: &fstest.MapFile{Data: []byte(ubtest.ReadFixture(t, path))},
	}
}

func TestReadProjectUsesProjectFile(t *testing.T) {
	m, err := ReadProject(fstest.MapFS{
		"project.ub": &fstest.MapFile{Data: []byte(
			ubtest.ReadValidFixture(t, "testdata/ub/project", "basic"),
		)},
	})
	require.NoError(t, err)
	require.Equal(t, "v0.2.0", m.UnobinVersion)
}

func TestReadProjectToolchainLine(t *testing.T) {
	m, err := ReadProject(projectFixtureFS(t, "testdata/ub/project/valid/basic.ub"))
	require.NoError(t, err)
	require.Equal(t, "v0.2.0", m.UnobinVersion)
}

func TestReadProjectWithoutToolchainLine(t *testing.T) {
	m, err := ReadProject(projectFixtureFS(t, "testdata/ub/project/valid/empty.ub"))
	require.NoError(t, err)
	require.Empty(t, m.UnobinVersion)
}

func TestReadProjectObjectRequirements(t *testing.T) {
	m, err := ReadProject(
		projectFixtureFS(t, "testdata/ub/project/valid/object-requirements.ub"))
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

func TestReadProjectFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/project", func(name string, src []byte) (string, []string) {
		m, err := ReadProject(fstest.MapFS{
			ProjectFileName: &fstest.MapFile{Data: src},
		})
		if err != nil {
			return "", []string{err.Error()}
		}
		out, err := lang.Canonicalize(ProjectFileName, EncodeProject(m))
		if err != nil {
			return "", []string{err.Error()}
		}
		return string(out), nil
	})
}

func TestReadProjectRejectsBadToolchainVersion(t *testing.T) {
	_, err := ReadProject(
		projectFixtureFS(t, "testdata/ub/project/invalid/bad-toolchain-version.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), `"latest" is not a valid version`)
}

func TestReadProjectRejectsNonStringToolchainLine(t *testing.T) {
	_, err := ReadProject(
		projectFixtureFS(t, "testdata/ub/project/invalid/non-string-toolchain.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unobin-version must be a string literal")
}

// TestReadProjectRejectsUnobinInRequires proves the unobin repo
// cannot be a floored dependency: its version is the toolchain's to
// pin, through the project's unobin-version line.
func TestReadProjectRejectsUnobinInRequires(t *testing.T) {
	_, err := ReadProject(projectFixtureFS(t, "testdata/ub/project/invalid/unobin-required.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolchain-versioned")
	require.Contains(t, err.Error(), "unobin-version line")
}

// TestEncodeProjectCanBeReadAgain pins the encoder as stable: its output
// parses, reading it back recovers the project, and re-encoding is
// byte-identical.
func TestEncodeProjectCanBeReadAgain(t *testing.T) {
	m := &Project{
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
	encoded := EncodeProject(m)

	back, err := ReadProject(fstest.MapFS{
		ProjectFileName: &fstest.MapFile{Data: encoded},
	})
	require.NoError(t, err)
	require.Equal(t, m.UnobinVersion, back.UnobinVersion)
	require.Equal(t, m.Requires, back.Requires)
	require.Equal(t, m.Replace, back.Replace)
	require.Equal(t, string(encoded), string(EncodeProject(back)),
		"re-encoding a read-back project should be byte-stable")
}

func TestEncodeProjectWritesToolchainLine(t *testing.T) {
	m := &Project{
		UnobinVersion: "v0.2.0",
		Requires:      map[Dependency]Requirement{},
	}
	encoded := EncodeProject(m)
	require.Contains(t, string(encoded), "unobin-version: 'v0.2.0'\n")

	back, err := ReadProject(fstest.MapFS{
		ProjectFileName: &fstest.MapFile{Data: encoded},
	})
	require.NoError(t, err)
	require.Equal(t, m.UnobinVersion, back.UnobinVersion)
}
