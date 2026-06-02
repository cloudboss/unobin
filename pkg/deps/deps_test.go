package deps

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDependency(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		want    Dependency
		wantErr bool
	}{
		{
			name: "url only",
			id:   "github.com/cloudboss/unobin",
			want: Dependency{URL: "github.com/cloudboss/unobin"},
		},
		{
			name: "url and subdir",
			id:   "github.com/cloudboss/unobin//pkg/libraries/core",
			want: Dependency{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/core"},
		},
		{
			name: "extra slashes around separator",
			id:   "github.com/x/y///sub",
			want: Dependency{URL: "github.com/x/y", Subdir: "sub"},
		},
		{name: "no slash in url", id: "github.com", wantErr: true},
		{name: "empty url before separator", id: "//sub", wantErr: true},
		{name: "empty", id: "", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseDependency(c.id)
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestDependencyStringRoundTrip(t *testing.T) {
	ids := []string{
		"github.com/cloudboss/unobin",
		"github.com/cloudboss/unobin//pkg/libraries/core",
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			dep, err := ParseDependency(id)
			require.NoError(t, err)
			assert.Equal(t, id, dep.String())
		})
	}
}

func TestReadManifest(t *testing.T) {
	fsys := fstest.MapFS{
		ManifestFileName: &fstest.MapFile{Data: []byte(`
requires: {
  'github.com/cloudboss/unobin//pkg/libraries/core': 'v0.1.0'
  'github.com/me/net//vpc':                          'v2.0.0'
}
`)},
	}
	m, err := ReadManifest(fsys)
	require.NoError(t, err)
	assert.Equal(t, map[Dependency]string{
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/core"}: "v0.1.0",
		{URL: "github.com/me/net", Subdir: "vpc"}:                          "v2.0.0",
	}, m.Requires)
}

func TestReadManifestEmptyRequires(t *testing.T) {
	fsys := fstest.MapFS{
		ManifestFileName: &fstest.MapFile{Data: []byte("requires: {}\n")},
	}
	m, err := ReadManifest(fsys)
	require.NoError(t, err)
	assert.Empty(t, m.Requires)
}

func TestReadManifestMissingFile(t *testing.T) {
	_, err := ReadManifest(fstest.MapFS{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestReadManifestRejectsBadVersionField(t *testing.T) {
	fsys := fstest.MapFS{
		ManifestFileName: &fstest.MapFile{Data: []byte("version: 'v1.0.0'\n")},
	}
	_, err := ReadManifest(fsys)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not a valid top level key for a manifest file")
}

func TestReadManifestRejectsBadDependencyURL(t *testing.T) {
	fsys := fstest.MapFS{
		ManifestFileName: &fstest.MapFile{Data: []byte("requires: { 'nohost': 'v1.0.0' }\n")},
	}
	_, err := ReadManifest(fsys)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repo URL must contain a host and a path")
}
