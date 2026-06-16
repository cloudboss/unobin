package resolve

import (
	"fmt"
	"os"
	"path/filepath"
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
	top, err := WalkUB(refs, &fakeUBResolver{}, v, nil)
	require.NoError(t, err)
	require.Len(t, top, 1)
	require.Equal(t, ResolutionGo, top[0].Kind)
	require.Equal(t, "github.com/x/unobin/core", top[0].Path)
	require.Equal(t, []string{"core=github.com/x/unobin/core@v0.1.0"}, v.goCalls)
	require.Empty(t, v.ubCalls)
}

func TestWalkUBRecordsUBLibrary(t *testing.T) {
	src := newUBSource(t, map[string]string{
		"library.ub": `
greeter: resource {
  description: 'g'
  imports: { core: 'github.com/x/unobin//core' }
  inputs: { name: { type: string } }
}
`,
	})
	refs := map[string]ImportRef{
		"hello": &RemoteImport{URL: "github.com/x/hello"},
	}
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/hello@v1.0.0": src,
	}}
	versions := map[string]string{
		"github.com/x/hello":  "v1.0.0",
		"github.com/x/unobin": "v0.1.0",
	}
	v := newRecordingVisitor()
	top, err := WalkUB(refs, r, v, versions)
	require.NoError(t, err)
	require.Len(t, top, 1)
	require.Equal(t, ResolutionUB, top[0].Kind)
	require.Equal(t, "remote:github.com/x/hello@v1.0.0", top[0].CanonicalKey)
	require.Equal(t, []string{"hello=remote:github.com/x/hello@v1.0.0"}, v.ubCalls)
	require.Equal(t, []string{"core=github.com/x/unobin/core@v0.1.0"}, v.goCalls)

	lib := v.ubLibs["remote:github.com/x/hello@v1.0.0"]
	require.NotNil(t, lib)
	require.Contains(t, lib.Bodies["resource"], "greeter")
	bodyImports := lib.BodyImports["resource"]["greeter"]
	require.Len(t, bodyImports, 1)
	require.Equal(t, ResolutionGo, bodyImports[0].Kind)
	require.Equal(t, "core", bodyImports[0].LocalAlias)
}

func TestWalkUBLocalGoLibraryGuidesToReplace(t *testing.T) {
	// A local-path import that resolves to a Go module (it has a go.mod)
	// cannot work; the error shows how to import by module path and replace.
	goLib := newUBSource(t, map[string]string{
		"go.mod": "module github.com/cloudboss/unobin-library-aws\n\ngo 1.26\n",
	})
	r := &fakeUBResolver{locals: map[string]*Source{"../../../..": goLib}}
	refs := map[string]ImportRef{"aws": &LocalImport{Path: "../../../.."}}
	_, err := WalkUB(refs, r, newRecordingVisitor(), nil)
	require.Error(t, err)
	msg := err.Error()
	require.Contains(t, msg, "is a Go library (module github.com/cloudboss/unobin-library-aws)")
	require.Contains(t, msg, "in the .ub file:")
	require.Contains(t, msg, "imports: { aws: 'github.com/cloudboss/unobin-library-aws' }")
	require.Contains(t, msg, "in manifest.ub:")
	require.Contains(t, msg,
		"manifest: { replace: { 'github.com/cloudboss/unobin-library-aws': '../../../..' } }")
}

func TestWalkUBLocalNonLibraryReports(t *testing.T) {
	// A local source that is neither a UB library nor a Go module.
	bare := newUBSource(t, map[string]string{"README.md": "hi"})
	r := &fakeUBResolver{locals: map[string]*Source{"./bare": bare}}
	refs := map[string]ImportRef{"bare": &LocalImport{Path: "./bare"}}
	_, err := WalkUB(refs, r, newRecordingVisitor(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not a UB library")
}

func TestWalkUBResolvesLocalBodyImportFromDeclaringPackage(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "greeter"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "helloer"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "greeter", "library.ub"), []byte(`
greeting: resource {
  imports: { helloer: '../helloer' }
  resources: { x: helloer.hello {} }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "helloer", "library.ub"), []byte(`
hello: resource {
  description: 'hello'
  resources: { x: local.file { path: '/tmp/x' } }
}
`), 0o644))

	refs := map[string]ImportRef{"greeter": &LocalImport{Path: "./greeter"}}
	v := newRecordingVisitor()

	_, err := WalkUB(refs, NewLocalResolver(root), v, nil)

	require.NoError(t, err)
	require.Len(t, v.ubCalls, 2)
	require.Contains(t, v.ubCalls[0], "helloer=")
	require.Contains(t, v.ubCalls[1], "greeter=")
}

func TestWalkUBResolvesFetchedLocalBodyImportFromSourceTree(t *testing.T) {
	parent := newUBSource(t, map[string]string{
		"library.ub": `
greeting: resource {
  imports: { child: './child' }
  resources: { x: child.hello {} }
}
`,
		"child/library.ub": `
hello: resource {
  description: 'hello'
  resources: { x: local.file { path: '/tmp/x' } }
}
`,
	})
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/parent@v1": parent,
	}}
	refs := map[string]ImportRef{"parent": &RemoteImport{URL: "github.com/x/parent"}}
	v := newRecordingVisitor()

	_, err := WalkUB(refs, r, v, map[string]string{"github.com/x/parent": "v1"})

	require.NoError(t, err)
	require.Len(t, v.ubCalls, 2)
	require.Contains(t, v.ubCalls[0], "child=")
	require.Contains(t, v.ubCalls[1], "parent=")
}

func TestWalkUBDedupsByCanonicalKey(t *testing.T) {
	src := newUBSource(t, map[string]string{
		"library.ub": "thing: resource { description: 'thing' inputs: { x: { type: string } } }\n",
	})
	refs := map[string]ImportRef{
		"a": &RemoteImport{URL: "github.com/x/y", Version: "v1.0.0"},
		"b": &RemoteImport{URL: "github.com/x/y", Version: "v1.0.0"},
	}
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/y@v1.0.0": src,
	}}
	v := newRecordingVisitor()
	top, err := WalkUB(refs, r, v, nil)
	require.NoError(t, err)
	require.Len(t, top, 2)
	require.Equal(t, top[0].CanonicalKey, top[1].CanonicalKey)
	require.Len(t, v.ubCalls, 1, "OnUBLibrary should fire once per canonical key")
	require.Equal(t, "a=remote:github.com/x/y@v1.0.0", v.ubCalls[0],
		"first alias by sort order should be the one passed to OnUBLibrary")
}

func TestWalkUBDetectsCycle(t *testing.T) {
	a := newUBSource(t, map[string]string{
		"library.ub": `
thing: resource {
  description: 't'
  imports: { b: 'github.com/x/b' }
  inputs: { x: { type: string } }
}
`,
	})
	b := newUBSource(t, map[string]string{
		"library.ub": `
other: resource {
  description: 'o'
  imports: { a: 'github.com/x/a' }
  inputs: { y: { type: string } }
}
`,
	})
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/a@v1": a,
		"github.com/x/b@v1": b,
	}}
	refs := map[string]ImportRef{
		"a": &RemoteImport{URL: "github.com/x/a"},
	}
	versions := map[string]string{
		"github.com/x/a": "v1",
		"github.com/x/b": "v1",
	}
	_, err := WalkUB(refs, r, newRecordingVisitor(), versions)
	require.Error(t, err)
	require.Contains(t, err.Error(), "import cycle")
}

func TestWalkUBPropagatesVisitorError(t *testing.T) {
	refs := map[string]ImportRef{
		"core": &RemoteImport{URL: "github.com/x/y", Version: "v1"},
	}
	v := newRecordingVisitor()
	v.failOn = "go:core"
	_, err := WalkUB(refs, &fakeUBResolver{}, v, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "forced failure")
}
