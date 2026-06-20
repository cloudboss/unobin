package root

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

// TestMain stamps a release version the way a real build's ldflags
// would, so tests model a released CLI; a test about development
// builds opts in with setCLIVersion(t, "dev").
func TestMain(m *testing.M) {
	Version = "v0.1.0"
	os.Exit(m.Run())
}

var commandRemoteSources = map[string]*resolve.Source{}

func runCommand(t *testing.T, args ...string) (string, error) {
	return runCommandWithRemotes(t, currentCommandRemotes(), args...)
}

func mergedCommandRemotes(remotes map[string]*resolve.Source) map[string]*resolve.Source {
	merged := currentCommandRemotes()
	if len(remotes) == 0 {
		return merged
	}
	if merged == nil {
		merged = map[string]*resolve.Source{}
	}
	maps.Copy(merged, remotes)
	return merged
}

func currentCommandRemotes() map[string]*resolve.Source {
	if len(commandRemoteSources) == 0 {
		return nil
	}
	out := make(map[string]*resolve.Source, len(commandRemoteSources))
	maps.Copy(out, commandRemoteSources)
	return out
}

func addCommandRemoteSource(t *testing.T, key string, src *resolve.Source) {
	t.Helper()
	prev, hadPrev := commandRemoteSources[key]
	commandRemoteSources[key] = src
	t.Cleanup(func() {
		if hadPrev {
			commandRemoteSources[key] = prev
		} else {
			delete(commandRemoteSources, key)
		}
	})
}

// runCommandWithRemotes is runCommand with a fake resolver that returns
// predefined Sources for the given remote URLs. Local imports keep working
// through the real LocalResolver.
func runCommandWithRemotes(t *testing.T, remotes map[string]*resolve.Source,
	args ...string) (string, error) {
	t.Helper()
	stubCompileResolver(t, mergedCommandRemotes(remotes))
	resetFlags(CompileCmd)
	resetFlags(PrintGraphCmd)
	resetFlags(depsSyncCmd)
	resetFlags(depsListCmd)
	resetFlags(depsVerifyCmd)
	resetFlags(depsGetCmd)
	root := &cobra.Command{
		Use:          "unobin",
		SilenceUsage: true,
	}
	root.AddCommand(VersionCmd)
	root.AddCommand(CompileCmd)
	root.AddCommand(PrintGraphCmd)
	root.AddCommand(DepsCmd)
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
	if src, found := r.remotes[remoteSourceKey(ri.URL, ri.Subdir, ri.Version)]; found {
		return sourceWithFS(src), nil
	}
	if ri.Subdir != "" {
		prefix := ri.Subdir + "/"
		version, ok := strings.CutPrefix(ri.Version, prefix)
		if ok {
			if src, found := r.remotes[remoteSourceKey(ri.URL, ri.Subdir, version)]; found {
				return sourceWithFS(src), nil
			}
		}
	}
	return nil, fmt.Errorf("fake resolver: no source for %s", remoteSourceKey(
		ri.URL, ri.Subdir, ri.Version))
}

func sourceWithFS(src *resolve.Source) *resolve.Source {
	if src == nil || src.FS != nil || src.Path == "" {
		return src
	}
	clone := *src
	clone.FS = os.DirFS(src.Path)
	return &clone
}

func remoteSourceKey(url, subdir, version string) string {
	key := url + "@" + version
	if subdir != "" {
		key = url + "//" + subdir + "@" + version
	}
	return key
}

func factorySource(body string) []byte {
	trimmed := strings.TrimPrefix(body, "\n")
	if strings.HasPrefix(strings.TrimSpace(trimmed), "factory:") {
		return []byte(body)
	}
	return []byte("factory: {\n" + trimmed + "}\n")
}

func validGoLibrarySource(pkg string) []byte {
	return fmt.Appendf(nil, `package %s

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{
		Resources: map[string]runtime.ResourceRegistration{
			"x": nil,
		},
	}
}
`, pkg)
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

func manifestSource(body string) []byte {
	return []byte("manifest: {\n" + body + "}\n")
}

func goCoreRemotes() map[string]*resolve.Source {
	goMod := &fstest.MapFile{Data: []byte("module github.com/x/core\n")}
	return map[string]*resolve.Source{
		"github.com/x/core@v1.0.0": {
			Commit: "abc123",
			FS:     fstest.MapFS{"go.mod": goMod},
		},
		"github.com/x/core//lib@v1.0.0": {
			Commit: "abc123",
			FS: fstest.MapFS{
				"go.mod": &fstest.MapFile{Data: []byte("module github.com/x/core/lib\n")},
				"lib.go": &fstest.MapFile{Data: []byte("package lib")},
			},
		},
	}
}

func stubListTags(t *testing.T, tags map[string][]string) {
	t.Helper()
	prev := depsListTags
	depsListTags = func(url string) ([]string, error) { return tags[url], nil }
	t.Cleanup(func() { depsListTags = prev })
}

func writeGetProject(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))
	stubListTags(t, map[string][]string{
		"github.com/x/core": {"lib/v1.0.0", "lib/v1.2.0", "lib/v2.0.0"},
	})
	return root
}

func goLibRemotes(version, commit string) map[string]*resolve.Source {
	return map[string]*resolve.Source{
		"github.com/x/core@" + version: {FS: fstest.MapFS{}},
		"github.com/x/core//lib@" + version: {
			Commit: commit,
			FS: fstest.MapFS{
				"go.mod": &fstest.MapFile{Data: []byte("module github.com/x/core/lib\n")},
				"lib.go": &fstest.MapFile{Data: []byte("package lib")},
			},
		},
	}
}

func writeScratchImportProject(t *testing.T, manifestBody string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), factorySource(`
imports: {
  scratch: 'github.com/x/scratch//ub/helloer'
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource(manifestBody), 0o644))
	return root
}

func scratchStdRemotes() map[string]*resolve.Source {
	scratchProject := scratchProjectFS()
	return map[string]*resolve.Source{
		"github.com/x/scratch@v0.8.0": {
			Commit: "scratch",
			FS:     scratchProject,
		},
		"github.com/x/scratch//ub/helloer@v0.8.0": {
			Commit: "scratch",
			FS: fstest.MapFS{
				"library.ub": scratchProject["ub/helloer/library.ub"],
			},
		},
		"github.com/x/std@v0.1.0": stdGoSource("v0.1.0"),
		"github.com/x/std@v0.2.0": stdGoSource("v0.2.0"),
	}
}

func scratchProjectFS() fstest.MapFS {
	return fstest.MapFS{
		deps.ManifestFileName: &fstest.MapFile{Data: manifestSource(`requires: {
  'github.com/x/std': { version: 'v0.1.0' }
}
`)},
		"ub/helloer/library.ub": &fstest.MapFile{Data: []byte(`hello: resource {
  imports: { std: 'github.com/x/std' }
  resources: { file: std.fs-file {} }
}
`)},
	}
}

func stdGoSource(version string) *resolve.Source {
	return &resolve.Source{
		Commit: "std-" + version,
		FS: fstest.MapFS{
			"go.mod": &fstest.MapFile{Data: []byte("module github.com/x/std\n")},
			"lib.go": &fstest.MapFile{Data: []byte("package std")},
		},
	}
}

// writeCompileLock writes a source lock into dir pinning each id (a
// repo//subdir or bare Go path) to a version. Compile takes versions from
// the lock, so a fixture with versionless imports needs one. Each entry is
// recorded as a Go library, which is all compile reads from the lock.
func writeCompileLock(t *testing.T, dir string, pins map[string]string) {
	t.Helper()
	lock := deps.NewLock()
	lock.ToolchainVersion = "dev"
	for id, version := range pins {
		lock.Deps[id] = &deps.LockedDep{Kind: deps.LockKindGo, Version: version, Commit: "c"}
		src := goTestSource(goModulePath(id))
		addCommandRemoteSource(t, id+"@"+version, src)
		addCommandRemoteSource(t, id+"@c", src)
	}
	require.NoError(t, deps.WriteSourceLock(filepath.Join(dir, deps.SourceLockFileName), lock))
}

func goModulePath(id string) string {
	return strings.Replace(id, "//", "/", 1)
}

func goTestSource(modulePath string) *resolve.Source {
	return &resolve.Source{FS: fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("module " + modulePath + "\n")},
		"lib.go": &fstest.MapFile{Data: []byte("package lib\n")},
	}}
}

// setCLIVersion stamps the CLI version for one test, the way a release
// build's ldflags would.
func setCLIVersion(t *testing.T, v string) {
	t.Helper()
	prev := Version
	Version = v
	t.Cleanup(func() { Version = prev })
}

func TestDepsSyncOutputCompilesForReplacedUnobinSubdir(t *testing.T) {
	rootDir := findUnobinRoot(t)
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"), []byte(`
factory: {
  imports: { cloud: 'github.com/cloudboss/unobin//examples/awscfg/cloud' }
  inputs: {
    cloud-config: {
      type:    library-config('github.com/cloudboss/unobin//examples/awscfg/cloud')
      default: {}
    }
  }
  library-configs: { cloud: var.cloud-config }
  actions: { describe: cloud.describe { label: 'world' } }
}
`), 0o644))
	manifest := "requires: {}\nreplace: {\n" +
		"  'github.com/cloudboss/unobin': '" + rootDir + "'\n" +
		"  'github.com/cloudboss/unobin//examples/awscfg': '" +
		filepath.Join(rootDir, "examples", "awscfg") + "'\n" +
		"}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ManifestFileName),
		manifestSource(manifest), 0o644))

	_, err := runCommand(t, "deps", "sync", "-p", filepath.Join(dir, "factory.ub"))
	require.NoError(t, err)
	synced, err := deps.ReadManifest(os.DirFS(dir))
	require.NoError(t, err)
	require.Empty(t, synced.Requires)
	_, err = runCommand(t, "compile", "-p", filepath.Join(dir, "factory.ub"), "-o", "-")
	require.NoError(t, err)
}

// TestDepsGetRefusesUnobin proves a floor cannot be added for the
// unobin repository.
func TestDepsGetRefusesUnobin(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
		factorySource("description: 'x'\n"), 0o644))

	_, err := runCommand(t, "deps", "get", "github.com/cloudboss/unobin@v0.5.0",
		"-p", filepath.Join(dir, "factory.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolchain-versioned")
}

// TestDepsSyncTeachesReplaceForUnobinImport proves sync does not ask
// for a floor on an unobin-repo import; the fix it teaches is the
// replace.
func TestDepsSyncTeachesReplaceForUnobinImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
		factorySource("imports: { x: 'github.com/cloudboss/unobin//examples/thing' }\n"), 0o644))

	_, err := runCommand(t, "deps", "sync", "-p", filepath.Join(dir, "factory.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolchain-versioned")
	require.NotContains(t, err.Error(), "deps get")
}

// TestCLIVersionFallsBackToBuildInfo proves an unstamped binary
// identifies by the module version Go recorded at install time, and a
// source build stays dev.
func TestCLIVersionFallsBackToBuildInfo(t *testing.T) {
	setCLIVersion(t, "dev")
	prev := readBuildInfo
	t.Cleanup(func() { readBuildInfo = prev })

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v0.4.2"}}, true
	}
	require.Equal(t, "v0.4.2", cliVersion())

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true
	}
	require.Equal(t, "dev", cliVersion())

	readBuildInfo = func() (*debug.BuildInfo, bool) { return nil, false }
	require.Equal(t, "dev", cliVersion())

	setCLIVersion(t, "v1.0.0")
	require.Equal(t, "v1.0.0", cliVersion())
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
	factoryPath := filepath.Join(srcDir, "factory.ub")
	require.NoError(t, os.WriteFile(factoryPath,
		factorySource("description: 'minimal'\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile",
		"-p", factoryPath,
		"-o", outDir,
		"--build",
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

// TestCompileBuildNoticesReplacedUnobin proves the post-tidy version
// check runs on the build path and reports the replacement rather than
// failing, since a replaced unobin is the development escape.
func TestCompileBuildNoticesReplacedUnobin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped: spawns `go build` and is slow")
	}
	rootDir := findUnobinRoot(t)

	srcDir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	factoryPath := filepath.Join(srcDir, "factory.ub")
	require.NoError(t, os.WriteFile(factoryPath,
		factorySource("description: 'minimal'\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	out, err := runCommand(t, "compile",
		"-p", factoryPath,
		"-o", outDir,
		"--build",
		"--replace-unobin", rootDir,
	)
	require.NoError(t, err)
	require.Contains(t, out, "github.com/cloudboss/unobin is replaced")
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

// compileLibrary writes a factory that imports a local library `lib`
// holding the given files (name -> body), runs compile without building,
// and returns the error. A floor or ceiling violation stops compile before
// any Go build, so no toolchain is needed.
func compileLibrary(t *testing.T, files map[string]string) error {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
		factorySource("imports: { lib: './lib' }\n"), 0o644))
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "lib", name), []byte(body), 0o644))
	}
	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "factory.ub"),
		"-o", outDir)
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
			file: "library.ub",
			body: "lookup: data { outputs: { v: { value: 'hi' } } }\n",
		},
		{
			name: "valid action composite",
			file: "library.ub",
			body: "deploy: action { actions: { c: core.command { argv: ['echo'] } } }\n",
		},
		{
			name: "valid resource composite",
			file: "library.ub",
			body: "box: resource { resources: { x: local.file { path: '/tmp/x' } } }\n",
		},
		{
			name: "data without output",
			file: "library.ub",
			body: "lookup: data { data: { x: aws.ami { most-recent: true } } }\n",
			wantErr: `import "lib": composite "lookup" (data): ` +
				`a data composite must declare at least one output`,
		},
		{
			name: "data with a resource",
			file: "library.ub",
			body: "lookup: data { resources: { x: local.file {} }\n" +
				"outputs: { id: { value: 'x' } } }\n",
			wantErr: `import "lib": composite "lookup" (data): ` +
				`a data composite must not contain resources`,
		},
		{
			name: "action without an action",
			file: "library.ub",
			body: "deploy: action { outputs: { v: { value: 'x' } } }\n",
			wantErr: `import "lib": composite "deploy" (action): ` +
				`an action composite must contain at least one action`,
		},
		{
			name: "resource without a resource",
			file: "library.ub",
			body: "box: resource { data: { x: aws.ami {} } }\n",
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
		"library.ub": `
a: data { data: { x: aws.ami {} } }
b: resource { data: { x: aws.ami {} } }
`,
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
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(stackSrc), 0o644))

	netDir := filepath.Join(dir, "libraries", "net")
	require.NoError(t, os.MkdirAll(netDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(netDir, "library.ub"), []byte(`
cluster: resource {
  description: 'a cluster'
  resources: { x: local.file { path: '/tmp/x', content: 'hi', mode: 420 } }
}
`), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", stackPath, "-o", outDir)
	require.NoError(t, err)

	wantMain := `// Code generated by unobin. DO NOT EDIT.
package main

import (
	lib_net "demo-factory/internal/net"
	"github.com/cloudboss/unobin/pkg/runner"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const (
	factoryBody        = "factory: {\n  imports: {\n    net: './libraries/net'\n  }\n}\n"
	factoryLibraryPath = ""
	factoryName        = "demo-factory"
)

// Stamped at link time via -ldflags.
var (
	factoryVersion  string
	contentRevision string
	unobinVersion   string
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
		UnobinVersion: unobinVersion,
	})
}
`
	mainBytes, err := os.ReadFile(filepath.Join(outDir, "main.go"))
	require.NoError(t, err)
	require.Equal(t, wantMain, string(mainBytes))

	pkgBytes, err := os.ReadFile(filepath.Join(outDir, "internal", "net", "net.go"))
	require.NoError(t, err)
	pkgSrc := string(pkgBytes)
	require.Contains(t, pkgSrc, "package net")
	require.Contains(t, pkgSrc, `Name: "net"`)
	require.Contains(t, pkgSrc, `ResourceComposites: map[string]*runtime.CompositeType{`)
	require.Contains(t, pkgSrc, `"cluster": {`)
	require.Regexp(t, `Kind:\s*runtime\.NodeResource`, pkgSrc)
	require.Contains(t, pkgSrc, `SyntaxBody: &syntax.FactoryBody{`)
	require.Contains(t, pkgSrc, `Name: syntax.Ident{Name: "x"}`)
	require.NotContains(t, pkgSrc, `Body: &lang.File{`)
}

func TestCompileWithSelfUBLibrary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"), []byte(`
factory: {
  imports: { self: '.' }
  data: { message: self.message {} }
  outputs: { text: { value: data.message.text } }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "library.ub"), []byte(`
message: data {
  outputs: { text: { value: 'hi' } }
}
`), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "factory.ub"), "-o", outDir)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(outDir, "internal", "self", "self.go"))
}

func TestCompileWithRemoteUBLibrary(t *testing.T) {
	libraryDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(libraryDir, "library.ub"), []byte(`
cluster: resource {
  description: 'a cluster'
  resources: { x: local.file { path: '/tmp/x', content: 'hi', mode: 420 } }
}
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports: {
  net: 'github.com/example/net//libraries/network'
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/example/net//libraries/network": "v1",
	})

	outDir := filepath.Join(t.TempDir(), "build")
	remotes := map[string]*resolve.Source{
		"github.com/example/net//libraries/network@v1": {
			FS:     os.DirFS(libraryDir),
			Commit: "abc123",
		},
	}
	_, err := runCommandWithRemotes(t, remotes, "compile",
		"-p", stackPath, "-o", outDir)
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

func TestCompileRejectsUBLockHashMismatch(t *testing.T) {
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
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports: { helloer: 'github.com/scratch/repo//ub/helloer' }
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
		"github.com/scratch/repo//ub/helloer@v0.8.0": {
			Commit: "c1",
			FS:     packageFS,
		},
		"github.com/scratch/repo//ub/helloer@c1": {
			Commit: "c1",
			FS:     packageFS,
		},
		"github.com/scratch/repo@c1": {
			Commit: "c1",
			FS:     rootFS,
		},
	}

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommandWithRemotes(t, remotes, "compile", "-p", stackPath, "-o", outDir)

	require.Error(t, err)
	require.Contains(t, err.Error(), "hash mismatch")
}

func TestCompileNestedUBLibraries(t *testing.T) {
	// inner library: a remote UB library the outer one imports.
	innerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "library.ub"), []byte(`
hello: resource {
  description: 'inner hello'
  inputs: { path: { type: string } }
  imports: { std: 'github.com/cloudboss/unobin-library-std' }
  resources: { this: std.file { path: var.path, content: 'hi' } }
  outputs: { path: { value: resource.this.path } }
}
`), 0o644))

	// outer library: imports inner under a different alias and wraps it.
	outerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outerDir, "library.ub"), []byte(`
greeting: resource {
  description: 'outer greeting'
  inputs: { path: { type: string } }
  imports: { inner: 'github.com/example/inner//ub/inner' }
  resources: { x: inner.hello { path: var.path } }
  outputs: { path: { value: resource.x.path } }
}
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports: {
  outer: 'github.com/example/outer//ub/outer'
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/example/outer//ub/outer":      "v1",
		"github.com/example/inner//ub/inner":      "v1",
		"github.com/cloudboss/unobin-library-std": "v0.1.0",
	})

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
		"-p", stackPath, "-o", outDir)
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

	// Inner's generated source binds "std" to the standard library
	// package.
	require.Contains(t, string(innerBytes),
		`lib_unobin_library_std "github.com/cloudboss/unobin-library-std"`)
	require.Contains(t, string(innerBytes),
		`"std": lib_unobin_library_std.Library()`)

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

func TestCompileDetectsUBImportCycle(t *testing.T) {
	// Library A's body imports library B; library B's body imports library
	// A. Compile must report the cycle rather than recurse forever.
	aDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(aDir, "library.ub"), []byte(`
type-a: resource {
  description: 'a body'
  imports: { b: 'github.com/example/b//ub/b' }
  resources: { y: b.type-b {} }
}
`), 0o644))

	bDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bDir, "library.ub"), []byte(`
type-b: resource {
  description: 'b body'
  imports: { a: 'github.com/example/a//ub/a' }
  resources: { z: a.type-a {} }
}
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports: {
  a: 'github.com/example/a//ub/a'
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/example/a//ub/a": "v1",
		"github.com/example/b//ub/b": "v1",
	})

	outDir := filepath.Join(t.TempDir(), "build")
	remotes := map[string]*resolve.Source{
		"github.com/example/a//ub/a@v1": {FS: os.DirFS(aDir), Commit: "a"},
		"github.com/example/b//ub/b@v1": {FS: os.DirFS(bDir), Commit: "b"},
	}
	_, err := runCommandWithRemotes(t, remotes, "compile",
		"-p", stackPath, "-o", outDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "import cycle")
}

func TestCompileSharesPackageAcrossAliases(t *testing.T) {
	// One UB library imported under different aliases from different
	// sites should generate exactly one Go package and both call sites
	// should bind to it.
	innerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "library.ub"), []byte(`
hello: resource {
  description: 'inner hello'
  inputs: { path: { type: string } }
  resources: { x: local.file { path: var.path, content: 'hi' } }
  outputs: { path: { value: resource.x.path } }
}
`), 0o644))

	wrapDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(wrapDir, "library.ub"), []byte(`
greeting: resource {
  description: 'wrap greeting'
  inputs: { path: { type: string } }
  imports: { inside: 'github.com/example/shared//ub/shared' }
  resources: { x: inside.hello { path: var.path } }
  outputs: { path: { value: resource.x.path } }
}
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	// Stack root uses alias "shared"; the wrapper composite uses
	// alias "inside" for the same URL.
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports: {
  shared: 'github.com/example/shared//ub/shared'
  wrap:   'github.com/example/wrap//ub/wrap'
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/example/shared//ub/shared": "v1",
		"github.com/example/wrap//ub/wrap":     "v1",
	})

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
		"-p", stackPath, "-o", outDir)
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

func TestCompileLocalNonUBLibraryFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports: {
  bare: './bare'
}
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bare"), 0o755))

	_, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a UB library")
}
