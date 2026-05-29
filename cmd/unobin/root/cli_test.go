package root

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	resetFlags(PrintGraphCmd)
	root := &cobra.Command{
		Use:          "unobin",
		SilenceUsage: true,
	}
	root.AddCommand(VersionCmd)
	root.AddCommand(CompileCmd)
	root.AddCommand(FetchCmd)
	root.AddCommand(PrintGraphCmd)
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
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
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
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	src := `
imports: {
  core: 'github.com/cloudboss/unobin/pkg/libraries/core@v0.1.0'
}
actions: {
  core: { command: { hi: { argv: ['echo', 'hi'] } } }
}
`
	require.NoError(t, os.WriteFile(stackPath, []byte(src), 0o644))

	out, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.NoError(t, err)

	require.Contains(t, out, "package main")
	require.Contains(t, out, `factoryName        = "demo-factory"`)
	require.Contains(t, out, "var (\n\tfactoryVersion  string\n\tcontentRevision string\n)")
	require.Contains(t, out, `"github.com/cloudboss/unobin/pkg/libraries/core"`)
}

func TestCompileWriteOut(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	src := `
imports: {
  core: 'github.com/cloudboss/unobin/pkg/libraries/core@v0.1.0'
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

	goModBytes, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(goModBytes), "module demo-factory")
	require.Contains(t, string(goModBytes), "github.com/cloudboss/unobin v0.1.0")
}

// TestCompileBuildStampsVersion compiles a minimal factory with --build
// and then runs the resulting binary's `version` subcommand to confirm
// that the factory version and content-revision were actually written
// into the linked binary. This catches the failure mode where the
// codegen template's `var factoryVersion` and the ldflags `-X main.<name>=`
// identifier go out of sync: a mismatch leaves the stamp variable
// empty, and the built binary reports no version.
func TestCompileBuildStampsVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped: spawns `go build` and is slow")
	}
	rootDir := findUnobinRoot(t)

	srcDir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	factoryPath := filepath.Join(srcDir, "main.ub")
	require.NoError(t, os.WriteFile(factoryPath,
		[]byte("description: 'minimal'\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile",
		"-p", factoryPath,
		"-o", outDir,
		"--build",
		"--unobin-version", "v0.0.0",
		"--replace-unobin", rootDir,
	)
	require.NoError(t, err)

	binaryPath := filepath.Join(outDir, "demo-factory")
	require.FileExists(t, binaryPath)

	out, err := exec.Command(binaryPath, "version").CombinedOutput()
	require.NoError(t, err, "version subcommand failed: %s", out)
	got := strings.TrimSpace(string(out))
	require.Contains(t, got, "demo-factory v0.0.0",
		"version output should carry the stamped factory version, got %q", got)
	require.Contains(t, got, "content-revision ",
		"version output should carry the stamped content-revision, got %q", got)
	require.NotContains(t, got, "content-revision )",
		"content-revision must not be empty (got %q); "+
			"the ldflags -X identifier and the codegen template var have drifted",
		got)
}

// findUnobinRoot walks up from the test's working directory looking
// for a go.mod naming the unobin module. The compile --build path
// needs this so it can pin the runtime via a local replace directive
// instead of going to the network.
func findUnobinRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	for d := cwd; ; d = filepath.Dir(d) {
		body, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil && strings.Contains(string(body), "module github.com/cloudboss/unobin") {
			return d
		}
		if d == filepath.Dir(d) {
			break
		}
	}
	t.Fatalf("could not find unobin go.mod above %s", cwd)
	return ""
}

func TestCompileRequiresOut(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte("description: 'x'"), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out")
}

func TestCompileMissingStackFile(t *testing.T) {
	_, err := runCommand(t, "compile", "-p", "/no/such/path/main.ub", "-o", "-")
	require.Error(t, err)
}

func TestCompileInvalidStackFails(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte("exports: { x: 'y.ub' }\n"), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
}

func TestCompileInvalidReferenceFails(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
resources: {
  local: {
    file: {
      bad: { path: var.missing }
    }
  }
}
`), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown input "missing"`)
}

func TestCompileUnimportedResourceModuleFails(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  local: 'github.com/cloudboss/unobin//pkg/libraries/local@v0.1.0'
}
resources: {
  greeter: {
    greeting: {
      welcome: { message: 'hello' }
    }
  }
}
`), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
	require.Contains(t, err.Error(), `library "greeter" is not imported`)
}

func TestCompileUnknownTrailingFieldFails(t *testing.T) {
	goModDir := writeFakeGoModule(t)

	dir := t.TempDir()
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  fake: 'example.com/fake@v0.1.0'
}
resources: {
  fake: {
    thing: {
      x: {}
    }
  }
}
outputs: {
  bad: { value: resource.fake.thing.x.nonexistent }
}
`), 0o644))

	remotes := map[string]*resolve.Source{
		"example.com/fake@v0.1.0": {Commit: "fakecommit", Path: goModDir},
	}
	_, err := runCommandWithRemotes(t, remotes,
		"compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown field "nonexistent"`)
	require.Contains(t, err.Error(), `fake.thing`)
}

func TestCompileAcceptsKnownTrailingField(t *testing.T) {
	goModDir := writeFakeGoModule(t)

	dir := t.TempDir()
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  fake: 'example.com/fake@v0.1.0'
}
resources: {
  fake: {
    thing: {
      x: {}
    }
  }
}
outputs: {
  good: { value: resource.fake.thing.x.id }
}
`), 0o644))

	remotes := map[string]*resolve.Source{
		"example.com/fake@v0.1.0": {Commit: "fakecommit", Path: goModDir},
	}
	_, err := runCommandWithRemotes(t, remotes,
		"compile", "-p", stackPath, "-o", "-")
	require.NoError(t, err)
}

// writeFakeGoModule writes a minimal Go library to a tmpdir that
// registers one resource type "thing" whose output struct lists `id`
// and `name`. The dev CLI's goschema walker parses this dir to
// learn the type's output schema.
func writeFakeGoModule(t *testing.T) string {
	t.Helper()
	goModDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(goModDir, "go.mod"),
		[]byte("module example.com/fake\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(goModDir, "library.go"), []byte(`package fake

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "fake",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput](),
		},
	}
}

type Thing struct{}

type ThingOutput struct {
	ID   string
	Name string
}
`), 0o644))
	return goModDir
}

func TestCompileWarnsWhenOutputTypeMissing(t *testing.T) {
	goModDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(goModDir, "go.mod"),
		[]byte("module example.com/partial\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(goModDir, "library.go"), []byte(`package partial

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "partial",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput](),
		},
	}
}

type Thing struct{}
`), 0o644))

	dir := t.TempDir()
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  partial: 'example.com/partial@v0.1.0'
}
resources: {
  partial: {
    thing: {
      x: {}
    }
  }
}
`), 0o644))

	remotes := map[string]*resolve.Source{
		"example.com/partial@v0.1.0": {Commit: "fakecommit", Path: goModDir},
	}
	out, err := runCommandWithRemotes(t, remotes,
		"compile", "-p", stackPath, "-o", "-")
	require.NoError(t, err)
	require.Contains(t, out, `warning: import "partial"`)
	require.Contains(t, out, "ThingOutput")
}

func TestCompileMalformedGoModuleFails(t *testing.T) {
	goModDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(goModDir, "go.mod"),
		[]byte("module example.com/broken\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(goModDir, "library.go"), []byte(`package broken

// no Library() function defined here -- the dev CLI should reject
// this import.
`), 0o644))

	dir := t.TempDir()
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  broken: 'example.com/broken@v0.1.0'
}
`), 0o644))

	remotes := map[string]*resolve.Source{
		"example.com/broken@v0.1.0": {Commit: "fakecommit", Path: goModDir},
	}
	_, err := runCommandWithRemotes(t, remotes,
		"compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
	require.Contains(t, err.Error(), `no Library()`)
}

// compileLibrary writes a factory that imports a local library `lib`
// holding the given files (name -> body), runs compile without building,
// and returns the error. A floor or ceiling violation stops compile before
// any Go build, so no toolchain is needed.
func compileLibrary(t *testing.T, files map[string]string) error {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"),
		[]byte("imports: { lib: './lib' }\n"), 0o644))
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "lib", name), []byte(body), 0o644))
	}
	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "main.ub"),
		"-o", outDir, "--unobin-version", "v0.1.0")
	return err
}

func TestCompileEnforcesCompositeFloors(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		body    string
		wantErr string
	}{
		{
			name: "valid pure data composite",
			file: "data-lookup.ub",
			body: "outputs: { v: { value: 'hi' } }\n",
		},
		{
			name: "valid action composite",
			file: "action-deploy.ub",
			body: "actions: { core: { command: { c: { argv: ['echo'] } } } }\n",
		},
		{
			name: "valid resource composite",
			file: "resource-box.ub",
			body: "resources: { local: { file: { x: { path: '/tmp/x' } } } }\n",
		},
		{
			name: "data without output",
			file: "data-lookup.ub",
			body: "data: { aws: { ami: { x: { most-recent: true } } } }\n",
			wantErr: `import "lib": composite "lookup" (data): ` +
				`a data composite must declare at least one output`,
		},
		{
			name: "data with a resource",
			file: "data-lookup.ub",
			body: "resources: { local: { file: { x: {} } } }\n" +
				"outputs: { id: { value: 'x' } }\n",
			wantErr: `import "lib": composite "lookup" (data): ` +
				`a data composite must not contain resources`,
		},
		{
			name: "action without an action",
			file: "action-deploy.ub",
			body: "outputs: { v: { value: 'x' } }\n",
			wantErr: `import "lib": composite "deploy" (action): ` +
				`an action composite must contain at least one action`,
		},
		{
			name: "resource without a resource",
			file: "resource-box.ub",
			body: "data: { aws: { ami: { x: {} } } }\n",
			wantErr: `import "lib": composite "box" (resource): ` +
				`a resource composite must contain at least one resource`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := compileLibrary(t, map[string]string{tt.file: tt.body})
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestCompileReportsAllCompositeViolationsInOrder(t *testing.T) {
	files := map[string]string{
		"data-a.ub":     "data: { aws: { ami: { x: {} } } }\n",
		"resource-b.ub": "data: { aws: { ami: { x: {} } } }\n",
	}
	want := `import "lib": composite "a" (data): a data composite must declare at least one output
composite "b" (resource): a resource composite must contain at least one resource`
	// The library's composites are held in a map; only the sort in the
	// compiler makes the reported order stable. Run several times so a
	// missing sort would show up as a flapping order.
	for range 3 {
		require.EqualError(t, compileLibrary(t, files), want)
	}
}

func TestCompileWithLocalUBLibrary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	stackSrc := `
imports: {
  net: './libraries/net'
}
`
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(stackSrc), 0o644))

	netDir := filepath.Join(dir, "libraries", "net")
	require.NoError(t, os.MkdirAll(netDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(netDir, "resource-cluster.ub"), []byte(`
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
	lib_net "demo-factory/internal/net"
	"github.com/cloudboss/unobin/pkg/runner"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const (
	factoryBody        = "\nimports: {\n  net: './libraries/net'\n}\n"
	factoryLibraryPath = ""
	factoryName        = "demo-factory"
)

// Stamped at link time via -ldflags.
var (
	factoryVersion  string
	contentRevision string
)

func main() {
	runner.Run(runner.Info{
		FactoryName:     factoryName,
		FactoryVersion:  factoryVersion,
		ContentRevision: contentRevision,
		FactoryBody:     factoryBody,
		LibraryPath:     factoryLibraryPath,
		Libraries: map[string]*runtime.Library{
			"net": lib_net.Library(),
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

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "net",
		ResourceComposites: map[string]*runtime.CompositeType{
			"cluster": {
				Name:     "cluster",
				Category: runtime.NodeResource,
				Body:     &lang.File{Kind: lang.FileExportedType, Path: "resource-cluster.ub", Body: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "description"}, Value: &lang.StringLit{Value: "a cluster"}}, {Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "resources"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "local"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "file"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "x"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "path"}, Value: &lang.StringLit{Value: "/tmp/x"}}, {Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "content"}, Value: &lang.StringLit{Value: "hi"}}, {Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "mode"}, Value: &lang.NumberLit{Value: "420", ParsedInt: 420}}}}}}}}}}}}}}}}},
			},
		},
	}
}
`
	pkgBytes, err := os.ReadFile(filepath.Join(outDir, "internal", "net", "net.go"))
	require.NoError(t, err)
	require.Equal(t, wantPkg, string(pkgBytes))
}

func TestCompileWithRemoteUBLibrary(t *testing.T) {
	libraryDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(libraryDir, "resource-cluster.ub"), []byte(`
description: 'a cluster'
resources: {
  local: { file: { x: { path: '/tmp/x', content: 'hi', mode: 420 } } }
}
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  net: 'github.com/example/net//libraries/network@v1'
}
`), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	remotes := map[string]*resolve.Source{
		"github.com/example/net//libraries/network@v1": {
			FS:     os.DirFS(libraryDir),
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
	require.Contains(t, string(mainBytes), `lib_net "demo-factory/internal/net"`)
	require.Contains(t, string(mainBytes), `"net": lib_net.Library()`)

	goModBytes, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.NotContains(t, string(goModBytes), "github.com/example/net",
		"a UB library remote should not appear as a Go import in go.mod")
}

func TestCompileNestedUBLibraries(t *testing.T) {
	// inner library: a remote UB library the outer one imports.
	innerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "resource-hello.ub"), []byte(`
description: 'inner hello'
inputs: { path: { type: string } }
imports: {
  local: 'github.com/cloudboss/unobin//pkg/libraries/local@v0.1.0'
}
resources: {
  local: { file: { this: { path: var.path, content: 'hi' } } }
}
outputs: { path: { value: resource.local.file.this.path } }
`), 0o644))

	// outer library: imports inner under a different alias and wraps it.
	outerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outerDir, "resource-greeting.ub"), []byte(`
description: 'outer greeting'
inputs: { path: { type: string } }
imports: {
  inner: 'github.com/example/inner//ub/inner@v1'
}
resources: {
  inner: { hello: { x: { path: var.path } } }
}
outputs: { path: { value: resource.inner.hello.x.path } }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
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
	// alias to the inner package's Library().
	require.Contains(t, string(outerBytes),
		`lib_inner "demo-factory/internal/inner"`,
		"outer should import the inner UB sub-package by its generated path")
	require.Contains(t, string(outerBytes),
		`"inner": lib_inner.Library()`,
		"outer's composite carries inner in its Libraries map")

	// Inner's generated source binds "local" to the unobin local
	// primitives package.
	require.Contains(t, string(innerBytes),
		`lib_local "github.com/cloudboss/unobin/pkg/libraries/local"`)
	require.Contains(t, string(innerBytes),
		`"local": lib_local.Library()`)

	// Stack root only imports outer; main.go does not see inner.
	mainBytes, err := os.ReadFile(filepath.Join(outDir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(mainBytes), `lib_outer "demo-factory/internal/outer"`)
	require.NotContains(t, string(mainBytes), "demo-factory/internal/inner",
		"the stack only references the outer library; inner is private to outer")

	// go.mod requires the unobin Go library pinned by inner's body.
	goModBytes, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(goModBytes),
		"github.com/cloudboss/unobin v0.1.0",
		"the Go library imported deep inside a composite is pinned in the stack go.mod")
}

func TestCompileRejectsConflictingGoVersions(t *testing.T) {
	// The stack root pins github.com/cloudboss/unobin at v0.1.0 via a
	// Go-library import. The composite body pins the same library at
	// v0.2.0. The stack ends up with two incompatible pins for the same
	// Go library path, which compile must reject up front rather than
	// letting `go build` discover it later.
	innerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "resource-cluster.ub"), []byte(`
description: 'a cluster'
imports: {
  local: 'github.com/cloudboss/unobin//pkg/libraries/local@v0.2.0'
}
resources: { local: { file: { x: { path: '/tmp/x', content: 'hi' } } } }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  net:   'github.com/example/net//libraries/network@v1'
  local: 'github.com/cloudboss/unobin//pkg/libraries/local@v0.1.0'
}
`), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	remotes := map[string]*resolve.Source{
		"github.com/example/net//libraries/network@v1": {
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
	// Library A's body imports library B; library B's body imports library
	// A. Compile must report the cycle rather than recurse forever.
	aDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(aDir, "resource-type-a.ub"), []byte(`
description: 'a body'
imports: { b: 'github.com/example/b//ub/b@v1' }
resources: { b: { type-b: { y: {} } } }
`), 0o644))

	bDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bDir, "resource-type-b.ub"), []byte(`
description: 'b body'
imports: { a: 'github.com/example/a//ub/a@v1' }
resources: { a: { type-a: { z: {} } } }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
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
	// One UB library imported under different aliases from different
	// sites should generate exactly one Go package and both call sites
	// should bind to it.
	innerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "resource-hello.ub"), []byte(`
description: 'inner hello'
inputs: { path: { type: string } }
resources: { local: { file: { x: { path: var.path, content: 'hi' } } } }
outputs: { path: { value: resource.local.file.x.path } }
`), 0o644))

	wrapDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(wrapDir, "resource-greeting.ub"), []byte(`
description: 'wrap greeting'
inputs: { path: { type: string } }
imports: {
  inside: 'github.com/example/shared//ub/shared@v1'
}
resources: { inside: { hello: { x: { path: var.path } } } }
outputs: { path: { value: resource.inside.hello.x.path } }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
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

	// The shared library appears once under its first-seen alias.
	entries, err := os.ReadDir(filepath.Join(outDir, "internal"))
	require.NoError(t, err)
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	require.ElementsMatch(t, []string{"shared", "wrap"}, names,
		"the shared library is generated once; the wrap library gets its own package")

	// The wrap package's composite Libraries map binds its local alias
	// "inside" to the shared package's Library().
	wrapBytes, err := os.ReadFile(filepath.Join(outDir, "internal", "wrap", "wrap.go"))
	require.NoError(t, err)
	require.Contains(t, string(wrapBytes),
		`lib_shared "demo-factory/internal/shared"`,
		"the wrap package imports the shared sub-package by its canonical path")
	require.Contains(t, string(wrapBytes),
		`"inside": lib_shared.Library()`,
		"wrap's composite-local alias `inside` resolves to the shared library")

	// main.go binds both stack-root aliases.
	mainBytes, err := os.ReadFile(filepath.Join(outDir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(mainBytes), `"shared": lib_shared.Library()`)
	require.Contains(t, string(mainBytes), `"wrap":   lib_wrap.Library()`)
}

func TestCompileReplaceUnobinUBSubdir(t *testing.T) {
	fakeUnobin := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(fakeUnobin, "some-lib"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(fakeUnobin, "some-lib", "resource-foo.ub"), []byte(`
description: 'a foo'
resources: { local: { file: { x: { path: '/tmp/x', content: 'hi', mode: 420 } } } }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  some: 'github.com/cloudboss/unobin//some-lib@v0.1.0'
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
	goModDir := filepath.Join(fakeUnobin, "pkg/libraries/local")
	require.NoError(t, os.MkdirAll(goModDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(goModDir, "library.go"),
		[]byte("package local\n\nfunc Library() any { return nil }\n"), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  local: 'github.com/cloudboss/unobin//pkg/libraries/local@v0.1.0'
}
`), 0o644))

	out, err := runCommand(t, "compile",
		"-p", stackPath, "-o", "-",
		"--version", "v0.1.0",
		"--replace-unobin", fakeUnobin)
	require.NoError(t, err)
	require.Contains(t, out, `"github.com/cloudboss/unobin/pkg/libraries/local"`)
}

func TestCompileReplaceUnobinMissingPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  local: 'github.com/cloudboss/unobin//pkg/libraries/local@v0.1.0'
}
`), 0o644))

	_, err := runCommand(t, "compile",
		"-p", stackPath, "-o", "-",
		"--replace-unobin", filepath.Join(t.TempDir(), "no-such-tree"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "replace github.com/cloudboss/unobin")
}

func TestCompileWithRemoteGoSubpath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  local: 'github.com/cloudboss/unobin//pkg/libraries/local@v0.1.0'
}
`), 0o644))

	out, err := runCommand(t, "compile", "-p", stackPath, "-o", "-",
		"--version", "v0.1.0")
	require.NoError(t, err)
	require.Contains(t, out, `"github.com/cloudboss/unobin/pkg/libraries/local"`)
}

func TestFetchResolvesLocalUBLibrary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  net: './libraries/net'
}
`), 0o644))

	netDir := filepath.Join(dir, "libraries", "net")
	require.NoError(t, os.MkdirAll(netDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(netDir, "resource-cluster.ub"), []byte(`
description: 'a cluster'
`), 0o644))

	out, err := runCommand(t, "fetch", "-p", stackPath)
	require.NoError(t, err)
	require.Contains(t, out, "net -> ./libraries/net (local)")
}

func TestFetchEmptyImports(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`description: 'x'`), 0o644))
	out, err := runCommand(t, "fetch", "-p", stackPath)
	require.NoError(t, err)
	require.Contains(t, out, "No imports")
}

func TestFetchMissingStack(t *testing.T) {
	_, err := runCommand(t, "fetch", "-p", "/no/such/path/main.ub")
	require.Error(t, err)
}

func TestCompileLocalNonUBLibraryFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  bare: './bare'
}
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bare"), 0o755))

	_, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a UB library")
}
