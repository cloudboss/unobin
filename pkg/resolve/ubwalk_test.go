package resolve

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// fakeUBResolver returns predefined Sources keyed by URL@version or
// LocalImport.Path. Anything not in the table comes back as an empty
// Source so it looks like a Go module.
type fakeUBResolver struct {
	remotes map[string]*Source
	locals  map[string]*Source
}

func (r *fakeUBResolver) Resolve(ref ImportRef) (*Source, error) {
	switch v := ref.(type) {
	case *RemoteImport:
		key := v.URL + "@" + v.Version
		if v.Subdir != "" {
			key = v.URL + "//" + v.Subdir + "@" + v.Version
		}
		if src, ok := r.remotes[key]; ok {
			return src, nil
		}
		return &Source{Commit: "go"}, nil
	case *LocalImport:
		if src, ok := r.locals[v.Path]; ok {
			return src, nil
		}
		return nil, fmt.Errorf("local source missing: %s", v.Path)
	}
	return nil, fmt.Errorf("unsupported ref type %T", ref)
}

type recordingVisitor struct {
	goCalls []string
	ubCalls []string
	ubLibs  map[string]*UBLibrary
	failOn  string
}

func newRecordingVisitor() *recordingVisitor {
	return &recordingVisitor{ubLibs: map[string]*UBLibrary{}}
}

func (v *recordingVisitor) OnGoImport(alias, path, version string) error {
	if v.failOn == "go:"+alias {
		return fmt.Errorf("forced failure on %s", alias)
	}
	v.goCalls = append(v.goCalls, fmt.Sprintf("%s=%s@%s", alias, path, version))
	return nil
}

func (v *recordingVisitor) OnUBLibrary(
	alias, key string, _ ImportRef, lib *UBLibrary,
) error {
	if v.failOn == "ub:"+alias {
		return fmt.Errorf("forced failure on %s", alias)
	}
	v.ubCalls = append(v.ubCalls, fmt.Sprintf("%s=%s", alias, key))
	v.ubLibs[key] = lib
	return nil
}

func newUBSource(t *testing.T, files map[string]string) *Source {
	t.Helper()
	mfs := fstest.MapFS{}
	for path, body := range files {
		mfs[path] = &fstest.MapFile{Data: []byte(body)}
	}
	return &Source{FS: mfs, Commit: "ub"}
}

func TestWalkUBRecordsGoImports(t *testing.T) {
	refs := map[string]ImportRef{
		"core": &RemoteImport{URL: "github.com/x/unobin", Subdir: "core", Version: "v0.1.0"},
	}
	v := newRecordingVisitor()
	top, err := WalkUB(refs, &fakeUBResolver{}, v)
	require.NoError(t, err)
	require.Len(t, top, 1)
	require.Equal(t, ResolutionGo, top[0].Kind)
	require.Equal(t, "github.com/x/unobin/core", top[0].Path)
	require.Equal(t, []string{"core=github.com/x/unobin/core@v0.1.0"}, v.goCalls)
	require.Empty(t, v.ubCalls)
}

func TestWalkUBRecordsUBLibrary(t *testing.T) {
	src := newUBSource(t, map[string]string{
		"resource-greeter.ub": `description: 'g'
imports: { core: 'github.com/x/unobin//core@v0.1.0' }
inputs: { name: { type: string } }
`,
	})
	refs := map[string]ImportRef{
		"hello": &RemoteImport{URL: "github.com/x/hello", Version: "v1.0.0"},
	}
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/hello@v1.0.0": src,
	}}
	v := newRecordingVisitor()
	top, err := WalkUB(refs, r, v)
	require.NoError(t, err)
	require.Len(t, top, 1)
	require.Equal(t, ResolutionUB, top[0].Kind)
	require.Equal(t, "remote:github.com/x/hello@v1.0.0", top[0].CanonicalKey)
	require.Equal(t, []string{"hello=remote:github.com/x/hello@v1.0.0"}, v.ubCalls)
	require.Equal(t, []string{"core=github.com/x/unobin/core@v0.1.0"}, v.goCalls)

	lib := v.ubLibs["remote:github.com/x/hello@v1.0.0"]
	require.NotNil(t, lib)
	require.Contains(t, lib.Bodies, "greeter")
	bodyImports := lib.BodyImports["greeter"]
	require.Len(t, bodyImports, 1)
	require.Equal(t, ResolutionGo, bodyImports[0].Kind)
	require.Equal(t, "core", bodyImports[0].LocalAlias)
}

func TestWalkUBDedupsByCanonicalKey(t *testing.T) {
	src := newUBSource(t, map[string]string{
		"resource-thing.ub": "description: 'thing'\ninputs: { x: { type: string } }\n",
	})
	refs := map[string]ImportRef{
		"a": &RemoteImport{URL: "github.com/x/y", Version: "v1.0.0"},
		"b": &RemoteImport{URL: "github.com/x/y", Version: "v1.0.0"},
	}
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/y@v1.0.0": src,
	}}
	v := newRecordingVisitor()
	top, err := WalkUB(refs, r, v)
	require.NoError(t, err)
	require.Len(t, top, 2)
	require.Equal(t, top[0].CanonicalKey, top[1].CanonicalKey)
	require.Len(t, v.ubCalls, 1, "OnUBLibrary should fire once per canonical key")
	require.Equal(t, "a=remote:github.com/x/y@v1.0.0", v.ubCalls[0],
		"first alias by sort order should be the one passed to OnUBLibrary")
}

func TestWalkUBDetectsCycle(t *testing.T) {
	a := newUBSource(t, map[string]string{
		"resource-thing.ub": `description: 't'
imports: { b: 'github.com/x/b@v1' }
inputs: { x: { type: string } }
`,
	})
	b := newUBSource(t, map[string]string{
		"resource-other.ub": `description: 'o'
imports: { a: 'github.com/x/a@v1' }
inputs: { y: { type: string } }
`,
	})
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/a@v1": a,
		"github.com/x/b@v1": b,
	}}
	refs := map[string]ImportRef{
		"a": &RemoteImport{URL: "github.com/x/a", Version: "v1"},
	}
	_, err := WalkUB(refs, r, newRecordingVisitor())
	require.Error(t, err)
	require.Contains(t, err.Error(), "import cycle")
}

func TestWalkUBPropagatesVisitorError(t *testing.T) {
	refs := map[string]ImportRef{
		"core": &RemoteImport{URL: "github.com/x/y", Version: "v1"},
	}
	v := newRecordingVisitor()
	v.failOn = "go:core"
	_, err := WalkUB(refs, &fakeUBResolver{}, v)
	require.Error(t, err)
	require.Contains(t, err.Error(), "forced failure")
}
