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

	"github.com/cloudboss/unobin/internal/ubtest"
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

func cliFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/cli", name)
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

func projectSource(body string) []byte {
	return []byte("project" + ": {\n" + body + "}\n")
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
	project := "requires: {}\nreplace: {\n" +
		"  'github.com/cloudboss/unobin': '" + rootDir + "'\n" +
		"  'github.com/cloudboss/unobin//examples/awscfg': '" +
		filepath.Join(rootDir, "examples", "awscfg") + "'\n" +
		"}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ProjectFileName),
		projectSource(project), 0o644))

	_, err := runCommand(t, "deps", "sync", "-p", filepath.Join(dir, "factory.ub"))
	require.NoError(t, err)
	synced, err := deps.ReadProject(os.DirFS(dir))
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
