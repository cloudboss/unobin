package resolve

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestResolveUBGraphReusesCanonicalLibraries(t *testing.T) {
	source := newUBFixtureSource(t, "ubwalk/valid/records-ub-library")
	before := newRecordingVisitor()
	refs := map[string]ImportRef{
		"a": &RemoteImport{URL: "github.com/x/hello"},
		"b": &RemoteImport{URL: "github.com/x/hello"},
	}
	resolver := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/hello@v1.0.0": source,
	}}
	graph, err := ResolveUBGraph(refs, resolver, before, map[string]string{
		"github.com/x/hello": "v1.0.0", "github.com/x/unobin": "v0.1.0",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, []string{
		graph.Top[0].LocalAlias, graph.Top[1].LocalAlias,
	})
	key := "remote:github.com/x/hello@v1.0.0"
	require.Equal(t, key, graph.Top[0].CanonicalKey)
	require.Equal(t, key, graph.Top[1].CanonicalKey)
	require.Equal(t, map[string]*UBLibrary{key: before.ubLibs[key]}, graph.Libraries)
	require.NoError(t, graph.ValidateSources())
	// Visiting needs only the parsed inventory, even after the source becomes unreadable.
	source.FS = fstest.MapFS{}
	after := newRecordingVisitor()
	require.NoError(t, graph.Visit(after))
	require.Equal(t, before.goCalls, after.goCalls)
	require.Equal(t, before.ubCalls, after.ubCalls)
	require.Same(t, before.ubLibs[key], after.ubLibs[key])
}

func TestImportGraphVisitEmpty(t *testing.T) {
	graph := &ImportGraph{}
	visitor := newRecordingVisitor()
	require.NoError(t, graph.Visit(visitor))
	require.Empty(t, visitor.goCalls)
	require.Empty(t, visitor.ubCalls)
	require.NoError(t, graph.ValidateSources())
}

func TestUBSourceDigestIncludesNamesAndContents(t *testing.T) {
	files := map[string][]byte{"a.ub": {1, 2}, "b.ub": {3}}
	require.Equal(t, sourceDigest(files), sourceDigest(map[string][]byte{
		"b.ub": {3}, "a.ub": {1, 2},
	}))
	require.NotEqual(t, sourceDigest(files), sourceDigest(map[string][]byte{
		"a.ub": {1}, "b.ub": {2, 3},
	}))
	require.NotEqual(t, sourceDigest(files), sourceDigest(map[string][]byte{
		"a.ub": {1, 2}, "c.ub": {3},
	}))
	require.NotEqual(t, sourceDigest(nil), sourceDigest(map[string][]byte{"a.ub": nil}))
}

func TestImportGraphVisitKeepsNestedErrorContext(t *testing.T) {
	source := newUBFixtureSource(t, "ubwalk/valid/records-ub-library")
	graph, err := ResolveUBGraph(map[string]ImportRef{
		"hello": &RemoteImport{URL: "github.com/x/hello"},
	}, &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/hello@v1.0.0": source,
	}}, newRecordingVisitor(), map[string]string{
		"github.com/x/hello": "v1.0.0", "github.com/x/unobin": "v0.1.0",
	}, nil)
	require.NoError(t, err)
	visitor := newRecordingVisitor()
	visitor.failOn = "go:core"
	require.EqualError(t, graph.Visit(visitor),
		`import "hello": composite "greeter": import "core": forced failure on core`)
}

func TestImportGraphRejectsChangedSourceInventory(t *testing.T) {
	body, err := fs.ReadFile(ubFixtureFS(t, "ubwalk/valid/records-ub-library"), "library.ub")
	require.NoError(t, err)
	for _, change := range []string{"edit", "add", "remove"} {
		t.Run(change, func(t *testing.T) {
			files := fstest.MapFS{"library.ub": {Data: body}}
			source := &Source{FS: files}
			graph, err := ResolveUBGraph(map[string]ImportRef{
				"hello": &RemoteImport{URL: "github.com/x/hello"},
			}, &fakeUBResolver{remotes: map[string]*Source{
				"github.com/x/hello@v1.0.0": source,
			}}, newRecordingVisitor(), map[string]string{
				"github.com/x/hello": "v1.0.0", "github.com/x/unobin": "v0.1.0",
			}, nil)
			require.NoError(t, err)
			require.NoError(t, graph.ValidateSources())
			switch change {
			case "edit":
				files["library.ub"] = &fstest.MapFile{Data: nil}
			case "add":
				files["extra.ub"] = &fstest.MapFile{Data: body}
			case "remove":
				delete(files, "library.ub")
			}
			require.ErrorContains(t, graph.ValidateSources(), "source changed")
		})
	}
}
