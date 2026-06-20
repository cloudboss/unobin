package root

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/require"
)

func writeStack(t *testing.T, dir, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(body), 0o644))
	return stackPath
}

func TestPrintGraphRejectsInvalidGoLibrary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph-invalid-go")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"), []byte(`factory: {
  imports: { bad: 'github.com/x/bad' }
}
`), 0o644))
	lock := deps.NewLock()
	lock.ToolchainVersion = "dev"
	lock.Deps["github.com/x/bad"] = &deps.LockedDep{
		Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "c1",
	}
	require.NoError(t, deps.WriteSourceLock(filepath.Join(dir, deps.SourceLockFileName), lock))
	badDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "go.mod"),
		[]byte("module github.com/x/bad\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "library.go"),
		[]byte("package bad\n\nfunc Library() any { return nil }\n"), 0o644))
	remotes := map[string]*resolve.Source{
		"github.com/x/bad@v1.0.0": {Commit: "c1", Path: badDir},
	}

	_, err := runCommandWithRemotes(t, remotes, "print-graph", "-p", dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must return *runtime.Library")
}

func TestPrintGraphRejectsSentinelWithoutReplacement(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph-sentinel")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ManifestFileName), []byte(
		"manifest: { requires: { 'example.com/lib': { version: '"+deps.ReplacementSentinel+"' } } }\n"),
		0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"), []byte(`factory: {
  imports: { lib: 'example.com/lib' }
}
`), 0o644))

	_, err := runCommand(t, "print-graph", "-p", dir)

	require.Error(t, err)
	require.Contains(t, err.Error(), "v0.0.0-unobin-replaced is reserved")
	require.NotContains(t, err.Error(), "fake resolver")
}

func TestPrintGraphRejectsUBLockHashMismatch(t *testing.T) {
	rootFS := fstest.MapFS{
		deps.ManifestFileName: &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
		"ub/helloer/library.ub": &fstest.MapFile{Data: []byte(`
hello: data {
  outputs: { message: { value: 'hi' } }
}
`)},
	}
	packageFS := fstest.MapFS{
		"library.ub": &fstest.MapFile{Data: []byte(`
hello: data {
  outputs: { message: { value: 'hi' } }
}
`)},
	}
	dir := filepath.Join(t.TempDir(), "graph")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"), []byte(`
factory: {
  imports: { helloer: 'github.com/scratch/repo//ub/helloer' }
}
`), 0o644))
	lock := deps.NewLock()
	lock.ToolchainVersion = "dev"
	lock.Deps["github.com/scratch/repo"] = &deps.LockedDep{
		Kind:    deps.LockKindUB,
		Version: "v0.8.0",
		Commit:  "c1",
		Hash:    "sha256:bad",
	}
	require.NoError(t, deps.WriteSourceLock(filepath.Join(dir, deps.SourceLockFileName), lock))
	remotes := map[string]*resolve.Source{
		"github.com/scratch/repo//ub/helloer@v0.8.0": {Commit: "c1", FS: packageFS},
		"github.com/scratch/repo//ub/helloer@c1":     {Commit: "c1", FS: packageFS},
		"github.com/scratch/repo@c1":                 {Commit: "c1", FS: rootFS},
	}

	_, err := runCommandWithRemotes(t, remotes, "print-graph", "-p", dir)

	require.Error(t, err)
	require.Contains(t, err.Error(), "hash mismatch")
}

func TestPrintGraphUsesReplacementSentinelForGoV2Module(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "lib")
	require.NoError(t, os.MkdirAll(moduleDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(`module example.com/lib/v2

go 1.26
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "library.go"),
		validGoLibrarySource("lib"), 0o644))
	manifest := "requires: {\n" +
		"  'example.com/lib/v2': { version: '" + deps.ReplacementSentinel + "' }\n" +
		"}\n" +
		"replace: { 'example.com/lib/v2': './lib' }\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource(manifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(`
factory: {
  imports: { lib: 'example.com/lib/v2' }
}
`), 0o644))

	out, err := runCommand(t, "print-graph", "-p", root)
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestPrintGraphExpandsLocalUBLibraryComposite(t *testing.T) {
	root := t.TempDir()

	greeterDir := filepath.Join(root, "greeter")
	require.NoError(t, os.MkdirAll(greeterDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(greeterDir, "library.ub"), []byte(`
greeting: resource {
  description: 'Greeting composite'
  inputs: { message: { type: string } }
  imports: { local: 'github.com/cloudboss/unobin//pkg/libraries/local' }
  resources: { this: local.file { path: '/tmp/greeting', content: var.message } }
  outputs: { path: { value: resource.this.path } }
}
`), 0o644))

	stackDir := filepath.Join(root, "stack")
	stackPath := writeStack(t, stackDir, `
inputs:  { who: { type: string } }
imports: { greeter: '../greeter' }
resources: { hello: greeter.greeting { message: var.who } }
`)
	writeCompileLock(t, stackDir, map[string]string{
		"github.com/cloudboss/unobin//pkg/libraries/local": "v0.1.0",
	})

	out, err := runCommand(t, "print-graph", "-p", stackPath)
	require.NoError(t, err)
	want := `resource.hello
  -> resource.hello/resource.this

resource.hello/resource.this
  -> var.who
`
	require.Equal(t, want, out)
}
