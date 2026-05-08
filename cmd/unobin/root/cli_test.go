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
	root := &cobra.Command{
		Use:          "unobin",
		SilenceUsage: true,
	}
	root.AddCommand(VersionCmd)
	root.AddCommand(CompileCmd)
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
			"cluster": {Name: "cluster", Body: &lang.File{Kind: lang.FileExportedType, Path: "cluster.ub", Body: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "description"}, Value: &lang.StringLit{Value: "a cluster"}}, {Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "resources"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "local"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "file"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "x"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "path"}, Value: &lang.StringLit{Value: "/tmp/x"}}, {Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "content"}, Value: &lang.StringLit{Value: "hi"}}, {Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "mode"}, Value: &lang.NumberLit{Value: "420", ParsedInt: 420}}}}}}}}}}}}}}}}}},
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
	require.Contains(t, string(pkgBytes), `"cluster": {Name: "cluster"`)

	mainBytes, err := os.ReadFile(filepath.Join(outDir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(mainBytes), `mod_net "demo-stack/internal/net"`)
	require.Contains(t, string(mainBytes), `"net": mod_net.Module()`)

	modBytes, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.NotContains(t, string(modBytes), "github.com/example/net",
		"a UB-module remote should not appear as a Go-import in go.mod")
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
	require.Contains(t, string(pkgBytes), `"foo": {Name: "foo"`)
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
