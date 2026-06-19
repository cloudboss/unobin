package deps

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func hashProject(t *testing.T, files fstest.MapFS) string {
	t.Helper()
	hash, err := HashUBProject(files)
	require.NoError(t, err)
	return hash
}

func TestHashUBProjectIncludesManifest(t *testing.T) {
	base := fstest.MapFS{
		"manifest.ub": &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
		"library.ub":  &fstest.MapFile{Data: []byte("thing: resource {}\n")},
	}
	changed := fstest.MapFS{
		"manifest.ub": &fstest.MapFile{Data: []byte(`manifest: {
  requires: { 'github.com/x/y': 'v1.0.0' }
}
`)},
		"library.ub": &fstest.MapFile{Data: []byte("thing: resource {}\n")},
	}

	require.NotEqual(t, hashProject(t, base), hashProject(t, changed))
}

func TestHashUBProjectExcludesNonProjectInputs(t *testing.T) {
	base := fstest.MapFS{
		"manifest.ub": &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
		"library.ub":  &fstest.MapFile{Data: []byte("thing: resource {}\n")},
	}
	withExtras := fstest.MapFS{
		"manifest.ub":        &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
		"library.ub":         &fstest.MapFile{Data: []byte("thing: resource {}\n")},
		"lock.ub":            &fstest.MapFile{Data: []byte("not parsed\n")},
		"stack.ub":           &fstest.MapFile{Data: []byte("stack: {}\n")},
		"notes.txt":          &fstest.MapFile{Data: []byte("ignored\n")},
		".hidden.ub":         &fstest.MapFile{Data: []byte("ignored: resource {}\n")},
		".hidden/lib.ub":     &fstest.MapFile{Data: []byte("ignored: resource {}\n")},
		"nested/manifest.ub": &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
		"nested/library.ub":  &fstest.MapFile{Data: []byte("ignored: resource {}\n")},
	}

	require.Equal(t, hashProject(t, base), hashProject(t, withExtras))
}

func TestHashUBProjectRejectsMalformedIncludedUB(t *testing.T) {
	_, err := HashUBProject(fstest.MapFS{
		"manifest.ub": &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
		"library.ub":  &fstest.MapFile{Data: []byte("thing: resource {\n")},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "library.ub")
}

func TestHashUBProjectRejectsMalformedNestedMarker(t *testing.T) {
	_, err := HashUBProject(fstest.MapFS{
		"manifest.ub":        &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
		"library.ub":         &fstest.MapFile{Data: []byte("thing: resource {}\n")},
		"nested/manifest.ub": &fstest.MapFile{Data: []byte("factory: {}\n")},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nested/manifest.ub")
}

func TestHashUBProjectRejectsSymlink(t *testing.T) {
	_, err := HashUBProject(fstest.MapFS{
		"manifest.ub": &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
		"library.ub":  &fstest.MapFile{Mode: fs.ModeSymlink},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "symlink")
}
