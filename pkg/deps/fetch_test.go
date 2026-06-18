package deps

import (
	"fmt"
	"strings"
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
	refs    []*resolve.RemoteImport
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
	r.refs = append(r.refs, ri)
	if src, ok := r.sources[srcKey(ri.URL, ri.Subdir, ri.Version)]; ok {
		return src, nil
	}
	if ri.Subdir != "" {
		prefix := ri.Subdir + "/"
		version, ok := strings.CutPrefix(ri.Version, prefix)
		if ok {
			if src, ok := r.sources[srcKey(ri.URL, ri.Subdir, version)]; ok {
				return src, nil
			}
		}
	}
	return nil, fmt.Errorf("no source for %s//%s@%s", ri.URL, ri.Subdir, ri.Version)
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

func TestFetchUsesPrefixedTagForSubdirProject(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "net", "net/v1.0.0"): {FS: fstest.MapFS{}},
		srcKey("github.com/x/y", "", "v1.0.0"):        {FS: fstest.MapFS{}},
	}}
	_, err := NewFetcher(r).Fetch(Dependency{URL: "github.com/x/y", Subdir: "net"}, "v1.0.0")
	require.NoError(t, err)
	require.NotEmpty(t, r.refs)
	assert.Equal(t, "net", r.refs[0].Subdir)
	assert.Equal(t, "net/v1.0.0", r.refs[0].Version)
}

func TestFetchReadsNearestParentManifest(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/libs", "ub/helloer", "ub/helloer/v1.0.0"): {
			FS: fstest.MapFS{"resource-hello.ub": &fstest.MapFile{Data: []byte(`
hello: resource {
  imports: { std: 'github.com/cloudboss/unobin-library-std' }
}
`)}},
		},
		srcKey("github.com/x/libs", "ub", "ub/v1.0.0"): {FS: fstest.MapFS{}},
		srcKey("github.com/x/libs", "", "v1.0.0"): {FS: fstest.MapFS{
			ManifestFileName: &fstest.MapFile{Data: []byte(`manifest: {
  requires: { 'github.com/cloudboss/unobin-library-std': 'v0.1.0' }
}
`)},
		}},
	}}

	got, err := NewFetcher(r).Fetch(
		Dependency{URL: "github.com/x/libs", Subdir: "ub/helloer"}, "v1.0.0")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, map[Dependency]string{
		{URL: "github.com/cloudboss/unobin-library-std"}: "v0.1.0",
	}, got.Requires)
}

func TestFetchReadsNearestSubdirManifest(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/libs", "ub/project-b/comprehensions",
			"ub/project-b/comprehensions/v0.1.0"): {FS: fstest.MapFS{}},
		srcKey("github.com/x/libs", "ub/project-b", "ub/project-b/v0.1.0"): {
			FS: fstest.MapFS{ManifestFileName: &fstest.MapFile{Data: []byte(`manifest: {
  requires: { 'github.com/x/project-dep': 'v0.2.0' }
}
`)}},
		},
		srcKey("github.com/x/libs", "ub", "ub/v0.1.0"): {FS: fstest.MapFS{}},
		srcKey("github.com/x/libs", "", "v0.1.0"): {FS: fstest.MapFS{
			ManifestFileName: &fstest.MapFile{Data: []byte(`manifest: {
  requires: { 'github.com/x/root-dep': 'v9.0.0' }
}
`)},
		}},
	}}

	got, err := NewFetcher(r).Fetch(
		Dependency{URL: "github.com/x/libs", Subdir: "ub/project-b/comprehensions"},
		"v0.1.0")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, map[Dependency]string{
		{URL: "github.com/x/project-dep"}: "v0.2.0",
	}, got.Requires)
}

func TestFetchPropagatesResolverError(t *testing.T) {
	r := &fakeResolver{sources: map[string]*resolve.Source{}}
	_, err := NewFetcher(r).Fetch(Dependency{URL: "github.com/x/y"}, "v1.0.0")
	require.Error(t, err)
}
