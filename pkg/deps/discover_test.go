package deps

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/ubtest"
)

func readDiscoverFixture(t testing.TB, name string) []byte {
	t.Helper()
	return []byte(ubtest.ReadValidFixture(t, "testdata/ub/discover", name))
}

func TestFindManifestDir(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	deep := filepath.Join(proj, "sub", "deep")
	require.NoError(t, os.MkdirAll(deep, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(proj, ManifestFileName), readDiscoverFixture(t, "empty-manifest"), 0o644))

	cases := []struct {
		name  string
		start string
	}{
		{"at the manifest dir", proj},
		{"one level up", filepath.Join(proj, "sub")},
		{"several levels up", deep},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FindManifestDir(c.start)
			require.NoError(t, err)
			assert.Equal(t, proj, got)
		})
	}
}

func TestFindManifestDirFromFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(root, ManifestFileName), readDiscoverFixture(t, "empty-manifest"), 0o644))
	mainUB := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(mainUB, []byte("description: 'x'\n"), 0o644))

	got, err := FindManifestDir(mainUB)
	require.NoError(t, err)
	assert.Equal(t, root, got)
}

func TestFindManifestDirNotFound(t *testing.T) {
	_, err := FindManifestDir(t.TempDir())
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestFindManifestDirIgnoresDirectoryNamedLikeManifest(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	require.NoError(t, os.MkdirAll(filepath.Join(child, ManifestFileName), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, ManifestFileName), readDiscoverFixture(t, "empty-manifest"), 0o644))

	got, err := FindManifestDir(child)
	require.NoError(t, err)
	assert.Equal(t, root, got)
}

func TestFindManifestDirRejectsWrongManifestRole(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	require.NoError(t, os.MkdirAll(child, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, ManifestFileName), readDiscoverFixture(t, "empty-manifest"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(child, ManifestFileName), readDiscoverFixture(t, "factory-role"), 0o644))

	_, err := FindManifestDir(child)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.ub must declare manifest")
}
