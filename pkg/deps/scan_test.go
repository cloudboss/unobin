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

func TestImportedReposPreservesSubdirs(t *testing.T) {
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
	repos, err := ImportedRepos(root)
	require.NoError(t, err)
	assert.Equal(t, map[Dependency]bool{
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/core"}:  true,
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/local"}: true,
	}, repos)
}

func TestImportedReposScansLocalLibraries(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "factory.ub"),
		"factory: { imports: { greeter: './greeter' } }\n")
	writeUB(t, filepath.Join(root, "greeter", "library.ub"), `
greeting: resource {
  imports: { helloer: 'github.com/scratch/repo//ub/helloer' }
}
`)
	repos, err := ImportedRepos(root)
	require.NoError(t, err)
	assert.Equal(t, map[Dependency]bool{
		{URL: "github.com/scratch/repo", Subdir: "ub/helloer"}: true,
	}, repos)
}

func TestImportedReposScansSourceDeclaredFactory(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "factory.ub"), `
factory: {
  imports: {
    core: 'github.com/cloudboss/unobin//pkg/libraries/core'
    greeter: './greeter'
  }
}
`)
	repos, err := ImportedRepos(root)
	require.NoError(t, err)
	assert.Equal(t, map[Dependency]bool{
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/core"}: true,
	}, repos)
}

func TestImportedReposValidatesSourceDeclaredFactory(t *testing.T) {
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
	_, err := ImportedRepos(root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `resource hello: meta key "@trigger" is not allowed`)
}

func TestImportedReposRejectsUntypedUBFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "loose.ub"), []byte(`
imports: { core: 'github.com/x/y' }
`), 0o644))

	_, err := ImportedRepos(root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot determine UB file role")
}

func TestImportedReposScansSourceDeclaredLibraryExports(t *testing.T) {
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
	repos, err := ImportedRepos(root)
	require.NoError(t, err)
	assert.Equal(t, map[Dependency]bool{
		{URL: "github.com/scratch/repo", Subdir: "ub/helloer"}: true,
	}, repos)
}

func TestImportedReposNoRemoteDeps(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "factory.ub"),
		"factory: { imports: { greeter: './greeter' } }\n")
	repos, err := ImportedRepos(root)
	require.NoError(t, err)
	assert.Empty(t, repos)
}

func TestImportedReposSkipsHiddenDirs(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "factory.ub"),
		"factory: { imports: { core: 'github.com/x/y//core' } }\n")
	writeUB(t, filepath.Join(root, ".git", "hooks", "stray.ub"),
		"imports: { bad: 'github.com/other/repo//lib' }\n")
	repos, err := ImportedRepos(root)
	require.NoError(t, err)
	assert.Equal(t, map[Dependency]bool{{URL: "github.com/x/y", Subdir: "core"}: true}, repos)
}
