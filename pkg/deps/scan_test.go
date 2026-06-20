package deps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeUB(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func TestImportedPackagesPreservesSubdirs(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "factory.ub"), `
factory: {
  imports: {
    core:    'github.com/cloudboss/unobin//pkg/libraries/core'
    local:   'github.com/cloudboss/unobin//pkg/libraries/local'
    greeter: './greeter'
  }
}
`)
	repos, err := ImportedPackages(root)
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/core"}:  true,
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/local"}: true,
	}, repos)
}

func TestImportedPackagesScansLocalLibraries(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "factory.ub"),
		"factory: { imports: { greeter: './greeter' } }\n")
	writeUB(t, filepath.Join(root, "greeter", "library.ub"), `
greeting: resource {
  imports: { helloer: 'github.com/scratch/repo//ub/helloer' }
}
`)
	repos, err := ImportedPackages(root)
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{
		{URL: "github.com/scratch/repo", Subdir: "ub/helloer"}: true,
	}, repos)
}

func TestImportedPackagesScansSourceDeclaredFactory(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "factory.ub"), `
factory: {
  imports: {
    core: 'github.com/cloudboss/unobin//pkg/libraries/core'
    greeter: './greeter'
  }
}
`)
	repos, err := ImportedPackages(root)
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/core"}: true,
	}, repos)
}

func TestImportedPackagesValidatesSourceDeclaredFactory(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "factory.ub"), `
factory: {
  resources: {
    hello: std.fs-file {
      @trigger: 'always'
    }
  }
}
`)
	_, err := ImportedPackages(root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `resource hello: meta key "@trigger" is not allowed`)
}

func TestImportedPackagesRejectsUntypedUBFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "loose.ub"), []byte(`
imports: { core: 'github.com/x/y' }
`), 0o644))

	_, err := ImportedPackages(root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot determine UB file role")
}

func TestImportedPackagesScansSourceDeclaredLibraryExports(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "library.ub"), `
greeting: resource {
  imports: {
    helloer: 'github.com/scratch/repo//ub/helloer'
  }
}

lookup: data {
  imports: {
    local: './local-data'
  }
}
`)
	repos, err := ImportedPackages(root)
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{
		{URL: "github.com/scratch/repo", Subdir: "ub/helloer"}: true,
	}, repos)
}

func TestImportedPackagesNoRemoteDeps(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "factory.ub"),
		"factory: { imports: { greeter: './greeter' } }\n")
	repos, err := ImportedPackages(root)
	require.NoError(t, err)
	assert.Empty(t, repos)
}

func TestImportedPackagesSkipsHiddenDirs(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "factory.ub"),
		"factory: { imports: { core: 'github.com/x/y//core' } }\n")
	writeUB(t, filepath.Join(root, ".git", "hooks", "stray.ub"),
		"imports: { bad: 'github.com/other/repo//lib' }\n")
	repos, err := ImportedPackages(root)
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{{URL: "github.com/x/y", Subdir: "core"}: true}, repos)
}

func TestImportedPackagesSkipsNestedProjects(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, ManifestFileName), "manifest: { requires: {} }\n")
	writeUB(t, filepath.Join(root, "factory-a", "factory.ub"), `
factory: {
  imports: { shared: 'example.com/shared//lib' }
}
`)
	writeUB(t, filepath.Join(root, "library-c", ManifestFileName),
		"manifest: { requires: {} }\n")
	writeUB(t, filepath.Join(root, "library-c", "abc.ub"), `
thing: resource {
  imports: { nested: 'example.com/nested//lib' }
}
`)

	repos, err := ImportedPackages(root)
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{
		{URL: "example.com/shared", Subdir: "lib"}: true,
	}, repos)
}

func TestImportedPackagesScansNestedProjectWhenStartedThere(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "library-c")
	writeUB(t, filepath.Join(root, ManifestFileName), "manifest: { requires: {} }\n")
	writeUB(t, filepath.Join(root, "factory.ub"), `
factory: {
  imports: { root: 'example.com/root//lib' }
}
`)
	writeUB(t, filepath.Join(child, ManifestFileName), "manifest: { requires: {} }\n")
	writeUB(t, filepath.Join(child, "abc.ub"), `
thing: resource {
  imports: { nested: 'example.com/nested//lib' }
}
`)

	repos, err := ImportedPackages(child)
	require.NoError(t, err)
	assert.Equal(t, map[RemotePackage]bool{
		{URL: "example.com/nested", Subdir: "lib"}: true,
	}, repos)
}

func TestImportedPackagesRejectsInvalidNestedManifest(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, ManifestFileName), "manifest: { requires: {} }\n")
	writeUB(t, filepath.Join(root, "factory.ub"), "factory: {}\n")
	writeUB(t, filepath.Join(root, "library-c", ManifestFileName), "factory: {}\n")

	_, err := ImportedPackages(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "library-c/manifest.ub")
	assert.NotContains(t, err.Error(), "project marker ./manifest.ub")
}
