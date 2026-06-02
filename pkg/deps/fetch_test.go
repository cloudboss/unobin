package deps

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeResolver struct {
	sources map[string]*resolve.Source // keyed by "<url>@<tag>"
	lastRef *resolve.RemoteImport
}

func (r *fakeResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	ri := ref.(*resolve.RemoteImport)
	r.lastRef = ri
	src, ok := r.sources[ri.URL+"@"+ri.Version]
	if !ok {
		return nil, fmt.Errorf("no source for %s@%s", ri.URL, ri.Version)
	}
	return src, nil
}

func TestFetchReadsManifest(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{
		"github.com/x/y@v1.0.0": {FS: fstest.MapFS{
			ManifestFileName: &fstest.MapFile{
				Data: []byte("requires: { 'github.com/x/dep': 'v2.0.0' }\n"),
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
		"github.com/x/y@v1.0.0": {FS: fstest.MapFS{"y.go": &fstest.MapFile{Data: []byte("package y")}}},
	}}
	got, err := NewFetcher(r).Fetch(Dependency{URL: "github.com/x/y"}, "v1.0.0")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestFetchUsesPlainTagForRootProject(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{
		"github.com/x/y@v1.0.0": {FS: fstest.MapFS{}},
	}}
	_, err := NewFetcher(r).Fetch(Dependency{URL: "github.com/x/y"}, "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "github.com/x/y", r.lastRef.URL)
	assert.Equal(t, "", r.lastRef.Subdir)
	assert.Equal(t, "v1.0.0", r.lastRef.Version)
}

func TestFetchUsesPrefixedTagForSubdirProject(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{
		"github.com/x/y@net/v1.0.0": {FS: fstest.MapFS{}},
	}}
	_, err := NewFetcher(r).Fetch(Dependency{URL: "github.com/x/y", Subdir: "net"}, "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "net", r.lastRef.Subdir)
	assert.Equal(t, "net/v1.0.0", r.lastRef.Version)
}

func TestFetchPropagatesResolverError(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{}}
	_, err := NewFetcher(r).Fetch(Dependency{URL: "github.com/x/y"}, "v1.0.0")
	require.Error(t, err)
}
