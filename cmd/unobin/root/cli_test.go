package root

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/ubtest"
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

func readCLIFixture(name string) string {
	path := filepath.Join("testdata", "ub", "cli", "valid", name+".ub")
	body, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return string(body)
}

func cliFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/cli", name)
}

func factorySource(body string) []byte {
	trimmed := strings.TrimPrefix(body, "\n")
	if strings.HasPrefix(strings.TrimSpace(trimmed), "factory"+":") {
		return []byte(body)
	}
	return []byte("factory" + ": {\n" + trimmed + "}\n")
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
	return []byte("manifest" + ": {\n" + body + "}\n")
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
		factorySource(cliFixture(t, "get-project-imports")), 0o644))
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
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource(cliFixture(t, "scratch-import-body")), 0o644))
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
		"ub/helloer/library.ub": &fstest.MapFile{Data: []byte(readCLIFixture("scratch-library"))},
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
		[]byte(cliFixture(t, "awscfg-factory")), 0o644))
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
