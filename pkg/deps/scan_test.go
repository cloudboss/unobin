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

func TestManifestFromImportsGroupsByRepo(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "main.ub"), `
imports: {
  core:    'github.com/cloudboss/unobin//pkg/libraries/core@v0.1.0'
  local:   'github.com/cloudboss/unobin//pkg/libraries/local@v0.1.0'
  greeter: './greeter'
}
`)
	m, err := ManifestFromImports(root)
	require.NoError(t, err)
	assert.Equal(t, map[Dependency]string{
		{URL: "github.com/cloudboss/unobin"}: "v0.1.0",
	}, m.Requires)
}

func TestManifestFromImportsTakesHighestVersion(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "main.ub"), `
imports: {
  a: 'github.com/x/y//liba@v1.0.0'
  b: 'github.com/x/y//libb@v2.0.0'
}
`)
	m, err := ManifestFromImports(root)
	require.NoError(t, err)
	assert.Equal(t, map[Dependency]string{{URL: "github.com/x/y"}: "v2.0.0"}, m.Requires)
}

func TestManifestFromImportsScansLocalLibraries(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "main.ub"), "imports: { greeter: './greeter' }\n")
	writeUB(t, filepath.Join(root, "greeter", "resource-greeting.ub"),
		"imports: { helloer: 'github.com/scratch/repo//ub/helloer@v0.3.0' }\n")
	m, err := ManifestFromImports(root)
	require.NoError(t, err)
	assert.Equal(t, map[Dependency]string{
		{URL: "github.com/scratch/repo"}: "v0.3.0",
	}, m.Requires)
}

func TestManifestFromImportsNoRemoteDeps(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "main.ub"), "imports: { greeter: './greeter' }\n")
	m, err := ManifestFromImports(root)
	require.NoError(t, err)
	assert.Empty(t, m.Requires)
}

func TestManifestFromImportsRejectsNonVersionRef(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "main.ub"),
		"imports: { x: 'github.com/x/y//lib@main' }\n")
	_, err := ManifestFromImports(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a version tag")
}

func TestManifestFromImportsSkipsHiddenDirs(t *testing.T) {
	root := t.TempDir()
	writeUB(t, filepath.Join(root, "main.ub"),
		"imports: { core: 'github.com/x/y//core@v1.0.0' }\n")
	writeUB(t, filepath.Join(root, ".git", "hooks", "stray.ub"),
		"imports: { bad: 'github.com/other/repo//lib@v9.9.9' }\n")
	m, err := ManifestFromImports(root)
	require.NoError(t, err)
	assert.Equal(t, map[Dependency]string{{URL: "github.com/x/y"}: "v1.0.0"}, m.Requires)
}
