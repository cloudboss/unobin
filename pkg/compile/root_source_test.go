package compile

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootResolveSourceIncludesProjectMetadata(t *testing.T) {
	projectDir := t.TempDir()
	sourceDir := filepath.Join(projectDir, "factories", "app")
	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(projectDir, "metadata.txt"),
		[]byte("project"),
		0644,
	))

	source := rootResolveSource(projectDir, sourceDir)

	require.Equal(t, sourceDir, source.Path)
	require.Equal(t, projectDir, source.ProjectPath)
	require.Equal(t, "factories/app", source.PackageSubdir)
	metadata, err := fs.ReadFile(source.ProjectFS, "metadata.txt")
	require.NoError(t, err)
	require.Equal(t, "project", string(metadata))
}

func TestRootResolveSourceAtProjectRootHasEmptyPackageSubdir(t *testing.T) {
	projectDir := t.TempDir()

	source := rootResolveSource(projectDir, projectDir)

	require.Empty(t, source.PackageSubdir)
}
