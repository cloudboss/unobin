package projectmarker

import (
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func readMarkerFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func TestClassifyNoMarker(t *testing.T) {
	marker, err := ClassifyRoot(fstest.MapFS{})
	require.NoError(t, err)
	require.Equal(t, None, marker.Kind)
	require.Empty(t, marker.ModulePath)
}

func TestClassifyManifest(t *testing.T) {
	marker, err := ClassifyRoot(fstest.MapFS{
		"manifest.ub": &fstest.MapFile{
			Data: readMarkerFixture(t, "testdata/ub/markers/valid/manifest.ub"),
		},
	})
	require.NoError(t, err)
	require.Equal(t, UB, marker.Kind)
}

func TestClassifyGoMod(t *testing.T) {
	marker, err := ClassifyRoot(fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("module example.com/lib\n\ngo 1.26\n")},
	})
	require.NoError(t, err)
	require.Equal(t, Go, marker.Kind)
	require.Equal(t, "example.com/lib", marker.ModulePath)
}

func TestClassifyBothMarkers(t *testing.T) {
	_, err := ClassifyRoot(fstest.MapFS{
		"manifest.ub": &fstest.MapFile{
			Data: readMarkerFixture(t, "testdata/ub/markers/valid/manifest.ub"),
		},
		"go.mod": &fstest.MapFile{Data: []byte("module example.com/lib\n")},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "project marker . has both manifest.ub and go.mod")
}

func TestClassifyMarkerDirectory(t *testing.T) {
	_, err := ClassifyRoot(fstest.MapFS{
		"go.mod": &fstest.MapFile{Mode: fs.ModeDir},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "project marker ./go.mod is a directory")
}

func TestClassifyMalformedManifest(t *testing.T) {
	_, err := ClassifyRoot(fstest.MapFS{
		"manifest.ub": &fstest.MapFile{
			Data: readMarkerFixture(t, "testdata/ub/markers/invalid/factory.ub"),
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "project marker ./manifest.ub: must declare manifest")
}

func TestClassifyMalformedGoMod(t *testing.T) {
	_, err := ClassifyRoot(fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("not a go.mod\n")},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "project marker ./go.mod:")
}

func TestClassifyMissingGoModulePath(t *testing.T) {
	_, err := ClassifyRoot(fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("go 1.26\n")},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "project marker ./go.mod: missing module path")
}

func TestClassifyMarkerSymlink(t *testing.T) {
	_, err := ClassifyRoot(fstest.MapFS{
		"go.mod": &fstest.MapFile{Mode: fs.ModeSymlink},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "project marker ./go.mod is a symlink")
}

func TestClassifySubdir(t *testing.T) {
	marker, err := Classify(fstest.MapFS{
		"child/go.mod": &fstest.MapFile{Data: []byte("module example.com/child\n")},
	}, "child")
	require.NoError(t, err)
	require.Equal(t, Go, marker.Kind)
	require.Equal(t, "example.com/child", marker.ModulePath)
}
