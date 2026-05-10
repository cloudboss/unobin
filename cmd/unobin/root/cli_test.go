package root

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func runCommand(t *testing.T, args ...string) (string, error) {
	return runCommandWithRemotes(t, nil, args...)
}

// runCommandWithRemotes is runCommand with a fake resolver that
// returns predefined Sources for the given remote URLs. Anything not
// in remotes is returned as a Source with no FS, so compile treats
// it as a plain Go import. Local imports keep working through the
// real LocalResolver.
func runCommandWithRemotes(t *testing.T, remotes map[string]*resolve.Source,
	args ...string) (string, error) {
	t.Helper()
	stubCompileResolver(t, remotes)
	resetFlags(CompileCmd)
	resetFlags(FetchCmd)
	root := &cobra.Command{
		Use:          "unobin",
		SilenceUsage: true,
	}
	root.AddCommand(VersionCmd)
	root.AddCommand(CompileCmd)
	root.AddCommand(FetchCmd)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func stubCompileResolver(t *testing.T, remotes map[string]*resolve.Source) {
	t.Helper()
	prev := newCompileResolver
	newCompileResolver = func(stackDir string) (resolve.Resolver, error) {
		return &fakeResolver{
			local:   resolve.NewLocalResolver(stackDir),
			remotes: remotes,
		}, nil
	}
	t.Cleanup(func() { newCompileResolver = prev })
}

type fakeResolver struct {
	local   *resolve.LocalResolver
	remotes map[string]*resolve.Source
}

func (r *fakeResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	if li, ok := ref.(*resolve.LocalImport); ok {
		return r.local.Resolve(li)
	}
	ri, ok := ref.(*resolve.RemoteImport)
	if !ok {
		return nil, fmt.Errorf("fake resolver: unsupported ref type %T", ref)
	}
	key := ri.URL + "@" + ri.Version
	if ri.Subdir != "" {
		key = ri.URL + "//" + ri.Subdir + "@" + ri.Version
	}
	if src, found := r.remotes[key]; found {
		return src, nil
	}
	return &resolve.Source{Commit: "fakecommit"}, nil
}

func resetFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

func TestVersionPrintsVersion(t *testing.T) {
	prev := Version
	Version = "v1.2.3"
	defer func() { Version = prev }()

	out, err := runCommand(t, "version")
	require.NoError(t, err)
	require.Contains(t, out, "v1.2.3")
}

func TestCompileToStdout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	src := `
imports: {
  core: 'github.com/cloudboss/unobin/pkg/modules/core@v0.1.0'
}
actions: {
  core: { command: { hi: { argv: ['echo', 'hi'] } } }
}
`
	require.NoError(t, os.WriteFile(stackPath, []byte(src), 0o644))

	out, err := runCommand(t, "compile", "-p", stackPath, "-o", "-",
		"--version", "v0.1.0", "--commit", "abc")
	require.NoError(t, err)

	require.Contains(t, out, "package main")
	require.Contains(t, out, `stackName    = "demo-stack"`)
	require.Contains(t, out, `stackVersion = "v0.1.0"`)
	require.Contains(t, out, `stackCommit  = "abc"`)
	require.Contains(t, out, `"github.com/cloudboss/unobin/pkg/modules/core"`)
}

func TestCompileWriteOut(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	src := `
imports: {
  core: 'github.com/cloudboss/unobin/pkg/modules/core@v0.1.0'
}
`
	require.NoError(t, os.WriteFile(stackPath, []byte(src), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", stackPath, "-o", outDir,
		"--unobin-version", "v0.1.0")
	require.NoError(t, err)

	mainBytes, err := os.ReadFile(filepath.Join(outDir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(mainBytes), "package main")

	modBytes, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(modBytes), "module demo-stack")
	require.Contains(t, string(modBytes), "github.com/cloudboss/unobin v0.1.0")
}

func TestCompileRequiresOut(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte("description: 'x'"), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out")
}

func TestCompileMissingStackFile(t *testing.T) {
	_, err := runCommand(t, "compile", "-p", "/no/such/path/stack.ub", "-o", "-")
	require.Error(t, err)
}

func TestCompileInvalidStackFails(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte("exports: { x: 'y.ub' }\n"), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
}

func TestCompileWithLocalUBModule(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	stackSrc := `
imports: {
  net: './modules/net'
}
`
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(stackSrc), 0o644))

	netDir := filepath.Join(dir, "modules", "net")
	require.NoError(t, os.MkdirAll(netDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(netDir, "module.ub"), []byte(`
description: 'net primitives'
exports: { cluster: 'cluster.ub' }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(netDir, "cluster.ub"), []byte(`
description: 'a cluster'
resources: {
  local: {
    file: {
      x: { path: '/tmp/x', content: 'hi', mode: 420 }
    }
  }
}
`), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", stackPath, "-o", outDir,
		"--unobin-version", "v0.1.0")
	require.NoError(t, err)

	wantMain := `// Code generated by unobin. DO NOT EDIT.
package main

import (
	mod_net "demo-stack/internal/net"
	"github.com/cloudboss/unobin/pkg/runner"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const (
	stackSource  = "\nimports: {\n  net: './modules/net'\n}\n"
	stackName    = "demo-stack"
	stackVersion = "dev"
	stackCommit  = ""
)

func main() {
	runner.Run(runner.Info{
		StackName:    stackName,
		StackVersion: stackVersion,
		StackCommit:  stackCommit,
		StackSource:  stackSource,
		Modules: map[string]*runtime.Module{
			"net": mod_net.Module(),
		},
	})
}
`
	mainBytes, err := os.ReadFile(filepath.Join(outDir, "main.go"))
	require.NoError(t, err)
	require.Equal(t, wantMain, string(mainBytes))

	wantPkg := `// Code generated by unobin. DO NOT EDIT.
package net

import (
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Module() *runtime.Module {
	return &runtime.Module{
		Name:        "net",
		Description: "net primitives",
		Composites: map[string]*runtime.CompositeType{
			"cluster": {
				Name: "cluster",
				Body: &lang.File{Kind: lang.FileExportedType, Path: "cluster.ub", Body: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "description"}, Value: &lang.StringLit{Value: "a cluster"}}, {Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "resources"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "local"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "file"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "x"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "path"}, Value: &lang.StringLit{Value: "/tmp/x"}}, {Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "content"}, Value: &lang.StringLit{Value: "hi"}}, {Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "mode"}, Value: &lang.NumberLit{Value: "420", ParsedInt: 420}}}}}}}}}}}}}}}}},
			},
		},
	}
}
`
	pkgBytes, err := os.ReadFile(filepath.Join(outDir, "internal", "net", "net.go"))
	require.NoError(t, err)
	require.Equal(t, wantPkg, string(pkgBytes))
}

func TestCompileWithRemoteUBModule(t *testing.T) {
	moduleDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "module.ub"), []byte(`
description: 'remote net'
exports: { cluster: 'cluster.ub' }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "cluster.ub"), []byte(`
description: 'a cluster'
resources: {
  local: { file: { x: { path: '/tmp/x', content: 'hi', mode: 420 } } }
}
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  net: 'github.com/example/net//modules/network@v1'
}
`), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	remotes := map[string]*resolve.Source{
		"github.com/example/net//modules/network@v1": {
			FS:     os.DirFS(moduleDir),
			Commit: "abc123",
			Hash:   "sha256:fakehash",
		},
	}
	_, err := runCommandWithRemotes(t, remotes, "compile",
		"-p", stackPath, "-o", outDir, "--unobin-version", "v0.1.0")
	require.NoError(t, err)

	pkgBytes, err := os.ReadFile(filepath.Join(outDir, "internal", "net", "net.go"))
	require.NoError(t, err)
	require.Contains(t, string(pkgBytes), "package net")
	require.Regexp(t, `"cluster":\s*\{\s*Name:\s*"cluster"`, string(pkgBytes))

	mainBytes, err := os.ReadFile(filepath.Join(outDir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(mainBytes), `mod_net "demo-stack/internal/net"`)
	require.Contains(t, string(mainBytes), `"net": mod_net.Module()`)

	modBytes, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.NotContains(t, string(modBytes), "github.com/example/net",
		"a UB-module remote should not appear as a Go-import in go.mod")
}

func TestCompileNestedUBModules(t *testing.T) {
	// inner module: a remote UB module the outer one imports.
	innerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "module.ub"), []byte(`
description: 'inner module'
exports: { hello: 'hello.ub' }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "hello.ub"), []byte(`
description: 'inner hello'
inputs: { path: { type: string } }
imports: {
  local: 'github.com/cloudboss/unobin//pkg/modules/local@v0.1.0'
}
resources: {
  local: { file: { this: { path: var.path, content: 'hi' } } }
}
outputs: { path: resource.local.file.this.path }
`), 0o644))

	// outer module: imports inner under a different alias and wraps it.
	outerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outerDir, "module.ub"), []byte(`
description: 'outer module'
exports: { greeting: 'greeting.ub' }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outerDir, "greeting.ub"), []byte(`
description: 'outer greeting'
inputs: { path: { type: string } }
imports: {
  inner: 'github.com/example/inner//ub/inner@v1'
}
resources: {
  inner: { hello: { x: { path: var.path } } }
}
outputs: { path: resource.inner.hello.x.path }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  outer: 'github.com/example/outer//ub/outer@v1'
}
`), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	remotes := map[string]*resolve.Source{
		"github.com/example/outer//ub/outer@v1": {
			FS: os.DirFS(outerDir), Commit: "outer-commit",
		},
		"github.com/example/inner//ub/inner@v1": {
			FS: os.DirFS(innerDir), Commit: "inner-commit",
		},
	}
	_, err := runCommandWithRemotes(t, remotes, "compile",
		"-p", stackPath, "-o", outDir, "--unobin-version", "v0.1.0")
	require.NoError(t, err)

	// Both packages were emitted.
	outerBytes, err := os.ReadFile(filepath.Join(outDir, "internal", "outer", "outer.go"))
	require.NoError(t, err)
	innerBytes, err := os.ReadFile(filepath.Join(outDir, "internal", "inner", "inner.go"))
	require.NoError(t, err)

	// Outer's generated source binds the composite-local "inner"
	// alias to the inner package's Module().
	require.Contains(t, string(outerBytes),
		`mod_inner "demo-stack/internal/inner"`,
		"outer should import the inner UB sub-package by its generated path")
	require.Contains(t, string(outerBytes),
		`"inner": mod_inner.Module()`,
		"outer's composite carries inner in its Modules map")

	// Inner's generated source binds "local" to the unobin local
	// primitives package.
	require.Contains(t, string(innerBytes),
		`mod_local "github.com/cloudboss/unobin/pkg/modules/local"`)
	require.Contains(t, string(innerBytes),
		`"local": mod_local.Module()`)

	// Stack root only imports outer; main.go does not see inner.
	mainBytes, err := os.ReadFile(filepath.Join(outDir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(mainBytes), `mod_outer "demo-stack/internal/outer"`)
	require.NotContains(t, string(mainBytes), "demo-stack/internal/inner",
		"the stack only references the outer module; inner is private to outer")

	// go.mod requires the unobin Go module pinned by inner's body.
	modBytes, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(modBytes),
		"github.com/cloudboss/unobin v0.1.0",
		"the Go module imported deep inside a composite is pinned in the stack go.mod")
}

func TestCompileRejectsConflictingGoVersions(t *testing.T) {
	// The stack root pins github.com/cloudboss/unobin at v0.1.0 via a
	// Go-module import. The composite body pins the same module at
	// v0.2.0. The stack ends up with two incompatible pins for the same
	// Go module path, which compile must reject up front rather than
	// letting `go build` discover it later.
	innerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "module.ub"), []byte(`
description: 'remote net'
exports: { cluster: 'cluster.ub' }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "cluster.ub"), []byte(`
description: 'a cluster'
imports: {
  local: 'github.com/cloudboss/unobin//pkg/modules/local@v0.2.0'
}
resources: { local: { file: { x: { path: '/tmp/x', content: 'hi' } } } }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  net:   'github.com/example/net//modules/network@v1'
  local: 'github.com/cloudboss/unobin//pkg/modules/local@v0.1.0'
}
`), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	remotes := map[string]*resolve.Source{
		"github.com/example/net//modules/network@v1": {
			FS: os.DirFS(innerDir), Commit: "abc123",
		},
	}
	_, err := runCommandWithRemotes(t, remotes, "compile",
		"-p", stackPath, "-o", outDir, "--unobin-version", "v0.1.0")
	require.Error(t, err)
	require.Contains(t, err.Error(), "conflicting versions")
	require.Contains(t, err.Error(), "v0.1.0")
	require.Contains(t, err.Error(), "v0.2.0")
}

func TestCompileDetectsUBImportCycle(t *testing.T) {
	// Module A's body imports module B; module B's body imports module
	// A. Compile must report the cycle rather than recurse forever.
	aDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(aDir, "module.ub"), []byte(`
description: 'a'
exports: { type-a: 'type-a.ub' }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(aDir, "type-a.ub"), []byte(`
description: 'a body'
imports: { b: 'github.com/example/b//ub/b@v1' }
resources: { b: { type-b: { y: {} } } }
`), 0o644))

	bDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bDir, "module.ub"), []byte(`
description: 'b'
exports: { type-b: 'type-b.ub' }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(bDir, "type-b.ub"), []byte(`
description: 'b body'
imports: { a: 'github.com/example/a//ub/a@v1' }
resources: { a: { type-a: { z: {} } } }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  a: 'github.com/example/a//ub/a@v1'
}
`), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	remotes := map[string]*resolve.Source{
		"github.com/example/a//ub/a@v1": {FS: os.DirFS(aDir), Commit: "a"},
		"github.com/example/b//ub/b@v1": {FS: os.DirFS(bDir), Commit: "b"},
	}
	_, err := runCommandWithRemotes(t, remotes, "compile",
		"-p", stackPath, "-o", outDir, "--unobin-version", "v0.1.0")
	require.Error(t, err)
	require.Contains(t, err.Error(), "import cycle")
}

func TestCompileSharesPackageAcrossAliases(t *testing.T) {
	// One UB module imported under different aliases from different
	// sites should generate exactly one Go package and both call sites
	// should bind to it.
	innerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "module.ub"), []byte(`
description: 'shared inner'
exports: { hello: 'hello.ub' }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "hello.ub"), []byte(`
description: 'inner hello'
inputs: { path: { type: string } }
resources: { local: { file: { x: { path: var.path, content: 'hi' } } } }
outputs: { path: resource.local.file.x.path }
`), 0o644))

	wrapDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(wrapDir, "module.ub"), []byte(`
description: 'wrap'
exports: { greeting: 'greeting.ub' }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wrapDir, "greeting.ub"), []byte(`
description: 'wrap greeting'
inputs: { path: { type: string } }
imports: {
  inside: 'github.com/example/shared//ub/shared@v1'
}
resources: { inside: { hello: { x: { path: var.path } } } }
outputs: { path: resource.inside.hello.x.path }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	// Stack root uses alias "shared"; the wrapper composite uses
	// alias "inside" for the same URL.
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  shared: 'github.com/example/shared//ub/shared@v1'
  wrap:   'github.com/example/wrap//ub/wrap@v1'
}
`), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	remotes := map[string]*resolve.Source{
		"github.com/example/shared//ub/shared@v1": {
			FS: os.DirFS(innerDir), Commit: "shared",
		},
		"github.com/example/wrap//ub/wrap@v1": {
			FS: os.DirFS(wrapDir), Commit: "wrap",
		},
	}
	_, err := runCommandWithRemotes(t, remotes, "compile",
		"-p", stackPath, "-o", outDir, "--unobin-version", "v0.1.0")
	require.NoError(t, err)

	// The shared module appears once under its first-seen alias.
	entries, err := os.ReadDir(filepath.Join(outDir, "internal"))
	require.NoError(t, err)
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	require.ElementsMatch(t, []string{"shared", "wrap"}, names,
		"the shared module is generated once; the wrap module gets its own package")

	// The wrap package's composite Modules map binds its local alias
	// "inside" to the shared package's Module().
	wrapBytes, err := os.ReadFile(filepath.Join(outDir, "internal", "wrap", "wrap.go"))
	require.NoError(t, err)
	require.Contains(t, string(wrapBytes),
		`mod_shared "demo-stack/internal/shared"`,
		"the wrap package imports the shared sub-package by its canonical path")
	require.Contains(t, string(wrapBytes),
		`"inside": mod_shared.Module()`,
		"wrap's composite-local alias `inside` resolves to the shared module")

	// main.go binds both stack-root aliases.
	mainBytes, err := os.ReadFile(filepath.Join(outDir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(mainBytes), `"shared": mod_shared.Module()`)
	require.Contains(t, string(mainBytes), `"wrap":   mod_wrap.Module()`)
}

func TestCompileReplaceUnobinUBSubdir(t *testing.T) {
	fakeUnobin := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(fakeUnobin, "some-mod"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(fakeUnobin, "some-mod", "module.ub"), []byte(`
description: 'replaced module'
exports: { foo: 'foo.ub' }
`), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(fakeUnobin, "some-mod", "foo.ub"), []byte(`
description: 'a foo'
resources: { local: { file: { x: { path: '/tmp/x', content: 'hi', mode: 420 } } } }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  some: 'github.com/cloudboss/unobin//some-mod@v0.1.0'
}
`), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile",
		"-p", stackPath, "-o", outDir,
		"--unobin-version", "v0.1.0",
		"--replace-unobin", fakeUnobin)
	require.NoError(t, err)

	pkgBytes, err := os.ReadFile(filepath.Join(outDir, "internal", "some", "some.go"))
	require.NoError(t, err)
	require.Contains(t, string(pkgBytes), "package some")
	require.Regexp(t, `"foo":\s*\{\s*Name:\s*"foo"`, string(pkgBytes))
}

func TestCompileReplaceUnobinGoSubdir(t *testing.T) {
	fakeUnobin := t.TempDir()
	require.NoError(t, os.MkdirAll(
		filepath.Join(fakeUnobin, "pkg/modules/local"), 0o755))

	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  local: 'github.com/cloudboss/unobin//pkg/modules/local@v0.1.0'
}
`), 0o644))

	out, err := runCommand(t, "compile",
		"-p", stackPath, "-o", "-",
		"--version", "v0.1.0",
		"--replace-unobin", fakeUnobin)
	require.NoError(t, err)
	require.Contains(t, out, `"github.com/cloudboss/unobin/pkg/modules/local"`)
}

func TestCompileReplaceUnobinMissingPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  local: 'github.com/cloudboss/unobin//pkg/modules/local@v0.1.0'
}
`), 0o644))

	_, err := runCommand(t, "compile",
		"-p", stackPath, "-o", "-",
		"--replace-unobin", filepath.Join(t.TempDir(), "no-such-tree"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "replace github.com/cloudboss/unobin")
}

func TestCompileWithRemoteGoSubpath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  local: 'github.com/cloudboss/unobin//pkg/modules/local@v0.1.0'
}
`), 0o644))

	out, err := runCommand(t, "compile", "-p", stackPath, "-o", "-",
		"--version", "v0.1.0")
	require.NoError(t, err)
	require.Contains(t, out, `"github.com/cloudboss/unobin/pkg/modules/local"`)
}

func TestFetchResolvesLocalUBModule(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  net: './modules/net'
}
`), 0o644))

	netDir := filepath.Join(dir, "modules", "net")
	require.NoError(t, os.MkdirAll(netDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(netDir, "module.ub"), []byte(`
description: 'net primitives'
exports: {}
`), 0o644))

	out, err := runCommand(t, "fetch", "-p", stackPath)
	require.NoError(t, err)
	require.Contains(t, out, "net -> ./modules/net (local)")
}

func TestFetchEmptyImports(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`description: 'x'`), 0o644))
	out, err := runCommand(t, "fetch", "-p", stackPath)
	require.NoError(t, err)
	require.Contains(t, out, "No imports")
}

func TestFetchMissingStack(t *testing.T) {
	_, err := runCommand(t, "fetch", "-p", "/no/such/path/stack.ub")
	require.Error(t, err)
}

func TestCompileLocalNonUBModuleFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  bare: './bare'
}
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bare"), 0o755))

	_, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
	require.Contains(t, err.Error(), "module.ub")
}
