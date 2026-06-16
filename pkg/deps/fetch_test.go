package deps

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeResolver serves canned sources keyed by a remote ref's url, subdir,
// and version (or by path for local refs), and records the last remote ref
// it was asked for.
type fakeResolver struct {
	sources map[string]*resolve.Source
	locals  map[string]*resolve.Source
	lastRef *resolve.RemoteImport
}

func srcKey(url, subdir, version string) string {
	return url + "|" + subdir + "|" + version
}

func (r *fakeResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	if li, ok := ref.(*resolve.LocalImport); ok {
		src, found := r.locals[li.Path]
		if !found {
			return nil, fmt.Errorf("no local source for %s", li.Path)
		}
		return src, nil
	}
	ri := ref.(*resolve.RemoteImport)
	r.lastRef = ri
	src, ok := r.sources[srcKey(ri.URL, ri.Subdir, ri.Version)]
	if !ok {
		return nil, fmt.Errorf("no source for %s//%s@%s", ri.URL, ri.Subdir, ri.Version)
	}
	return src, nil
}

func TestFetchReadsManifest(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "", "v1.0.0"): {FS: fstest.MapFS{
			ManifestFileName: &fstest.MapFile{
				Data: []byte("manifest: { requires: { 'github.com/x/dep': 'v2.0.0' } }\n"),
			},
		}},
	}}
	got, err := NewFetcher(r).Fetch(Dependency{URL: "github.com/x/y"}, "v1.0.0")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t,
		map[Dependency]string{{URL: "github.com/x/dep"}: "v2.0.0"}, got.Requires)
}

func TestFetchLeafHasNoManifest(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "", "v1.0.0"): {
			FS: fstest.MapFS{"y.go": &fstest.MapFile{Data: []byte("package y")}},
		},
	}}
	got, err := NewFetcher(r).Fetch(Dependency{URL: "github.com/x/y"}, "v1.0.0")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestFetchUsesPlainTagForRootProject(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "", "v1.0.0"): {FS: fstest.MapFS{}},
	}}
	_, err := NewFetcher(r).Fetch(Dependency{URL: "github.com/x/y"}, "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "github.com/x/y", r.lastRef.URL)
	assert.Equal(t, "", r.lastRef.Subdir)
	assert.Equal(t, "v1.0.0", r.lastRef.Version)
}

func TestFetchUsesRepoTagForSubdirProject(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "net", "v1.0.0"): {FS: fstest.MapFS{}},
	}}
	_, err := NewFetcher(r).Fetch(Dependency{URL: "github.com/x/y", Subdir: "net"}, "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "net", r.lastRef.Subdir)
	assert.Equal(t, "v1.0.0", r.lastRef.Version)
}

func TestFetchPropagatesResolverError(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{}}
	_, err := NewFetcher(r).Fetch(Dependency{URL: "github.com/x/y"}, "v1.0.0")
	require.Error(t, err)
}
