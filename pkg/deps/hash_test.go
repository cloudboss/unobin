package deps

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/ubtest"
)

func hashProject(t *testing.T, files fstest.MapFS) string {
	t.Helper()
	hash, err := HashUBProject(files)
	require.NoError(t, err)
	return hash
}

func hashValidFixture(t testing.TB, name string) []byte {
	t.Helper()
	return []byte(ubtest.ReadValidFixture(t, "testdata/ub/hash", name))
}

func hashInvalidFixture(t testing.TB, name string) []byte {
	t.Helper()
	return []byte(ubtest.ReadFixture(t, "testdata/ub/hash/invalid/"+name+".ub"))
}

func TestHashUBProjectIncludesManifest(t *testing.T) {
	base := fstest.MapFS{
		"manifest.ub": &fstest.MapFile{Data: hashValidFixture(t, "empty-manifest")},
		"library.ub":  &fstest.MapFile{Data: hashValidFixture(t, "library-resource")},
	}
	changed := fstest.MapFS{
		"manifest.ub": &fstest.MapFile{Data: hashValidFixture(t, "manifest-with-requirement")},
		"library.ub":  &fstest.MapFile{Data: hashValidFixture(t, "library-resource")},
	}

	require.NotEqual(t, hashProject(t, base), hashProject(t, changed))
}

func TestHashUBProjectExcludesNonProjectInputs(t *testing.T) {
	base := fstest.MapFS{
		"manifest.ub": &fstest.MapFile{Data: hashValidFixture(t, "empty-manifest")},
		"library.ub":  &fstest.MapFile{Data: hashValidFixture(t, "library-resource")},
	}
	withExtras := fstest.MapFS{
		"manifest.ub":        &fstest.MapFile{Data: hashValidFixture(t, "empty-manifest")},
		"library.ub":         &fstest.MapFile{Data: hashValidFixture(t, "library-resource")},
		"lock.ub":            &fstest.MapFile{Data: []byte("not parsed\n")},
		"stack.ub":           &fstest.MapFile{Data: hashValidFixture(t, "stack")},
		"notes.txt":          &fstest.MapFile{Data: []byte("ignored\n")},
		".hidden.ub":         &fstest.MapFile{Data: hashValidFixture(t, "library-resource")},
		".hidden/lib.ub":     &fstest.MapFile{Data: hashValidFixture(t, "library-resource")},
		"nested/manifest.ub": &fstest.MapFile{Data: hashValidFixture(t, "empty-manifest")},
		"nested/library.ub":  &fstest.MapFile{Data: hashValidFixture(t, "library-resource")},
	}

	require.Equal(t, hashProject(t, base), hashProject(t, withExtras))
}

func TestHashUBProjectRejectsMalformedIncludedUB(t *testing.T) {
	_, err := HashUBProject(fstest.MapFS{
		"manifest.ub": &fstest.MapFile{Data: hashValidFixture(t, "empty-manifest")},
		"library.ub":  &fstest.MapFile{Data: hashInvalidFixture(t, "malformed-resource")},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "library.ub")
}

func TestHashUBProjectRejectsMalformedNestedMarker(t *testing.T) {
	_, err := HashUBProject(fstest.MapFS{
		"manifest.ub":        &fstest.MapFile{Data: hashValidFixture(t, "empty-manifest")},
		"library.ub":         &fstest.MapFile{Data: hashValidFixture(t, "library-resource")},
		"nested/manifest.ub": &fstest.MapFile{Data: hashValidFixture(t, "factory-marker")},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nested/manifest.ub")
}

func TestHashUBProjectRejectsSymlink(t *testing.T) {
	_, err := HashUBProject(fstest.MapFS{
		"manifest.ub": &fstest.MapFile{Data: hashValidFixture(t, "empty-manifest")},
		"library.ub":  &fstest.MapFile{Mode: fs.ModeSymlink},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "symlink")
}
