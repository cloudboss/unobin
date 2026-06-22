package deps

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scanFixtureRoot(t testing.TB, name string) string {
	t.Helper()
	root := filepath.Join("testdata/ub/scan", filepath.FromSlash(name))
	require.DirExists(t, root)
	return root
}

func TestImportedPackagesPreservesSubdirs(t *testing.T) {
	repos, err := ImportedPackages(scanFixtureRoot(t, "valid/preserves-subdirs"))
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/core"}:  true,
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/local"}: true,
	}, repos)
}

func TestImportedPackagesScansLocalLibraries(t *testing.T) {
	repos, err := ImportedPackages(scanFixtureRoot(t, "valid/scans-local-libraries"))
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{
		{URL: "github.com/scratch/repo", Subdir: "ub/helloer"}: true,
	}, repos)
}

func TestImportedPackagesScansSourceDeclaredFactory(t *testing.T) {
	repos, err := ImportedPackages(scanFixtureRoot(t, "valid/scans-source-declared-factory"))
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/core"}: true,
	}, repos)
}

func TestImportedPackagesScansSourceDeclaredLibraryExports(t *testing.T) {
	repos, err := ImportedPackages(scanFixtureRoot(t, "valid/scans-source-declared-library-exports"))
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{
		{URL: "github.com/scratch/repo", Subdir: "ub/helloer"}: true,
	}, repos)
}

func TestImportedPackagesNoRemoteDeps(t *testing.T) {
	repos, err := ImportedPackages(scanFixtureRoot(t, "valid/no-remote-deps"))
	require.NoError(t, err)
	assert.Empty(t, repos)
}

func TestImportedPackagesSkipsHiddenDirs(t *testing.T) {
	repos, err := ImportedPackages(scanFixtureRoot(t, "valid/skips-hidden-dirs"))
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{{URL: "github.com/x/y", Subdir: "core"}: true}, repos)
}

func TestImportedPackagesSkipsNestedProjects(t *testing.T) {
	repos, err := ImportedPackages(scanFixtureRoot(t, "valid/skips-nested-projects"))
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{
		{URL: "example.com/shared", Subdir: "lib"}: true,
	}, repos)
}

func TestImportedPackagesScansNestedProjectWhenStartedThere(t *testing.T) {
	root := scanFixtureRoot(t, "valid/scans-nested-project-when-started-there")
	repos, err := ImportedPackages(filepath.Join(root, "library-c"))
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{
		{URL: "example.com/nested", Subdir: "lib"}: true,
	}, repos)
}

func TestImportedPackagesRejectsInvalidNestedManifest(t *testing.T) {
	_, err := ImportedPackages(scanFixtureRoot(t, "invalid/invalid-nested-manifest"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "library-c/manifest.ub")
	assert.NotContains(t, err.Error(), "project marker ./manifest.ub")
}
