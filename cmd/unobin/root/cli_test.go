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

	compilepkg "github.com/cloudboss/unobin/pkg/compile"
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

func TestDepsSyncAddsLockedVersionForDirectImport(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource("imports: { core: 'github.com/x/core' }\n"), 0o644))
	lock := deps.NewLock()
	lock.ToolchainVersion = "v0.1.0"
	lock.Deps["github.com/x/core"] = &deps.LockedDep{
		Kind: deps.LockKindGo, Version: "v1.2.0", Commit: "c12",
	}
	require.NoError(t, deps.WriteSourceLock(filepath.Join(root, deps.SourceLockFileName), lock))
	remotes := map[string]*resolve.Source{
		"github.com/x/core@v1.2.0": {
			Commit: "c12",
			FS: fstest.MapFS{
				"go.mod": &fstest.MapFile{Data: []byte("module github.com/x/core\n")},
				"lib.go": &fstest.MapFile{Data: []byte("package core\n")},
			},
		},
	}

	_, err := runCommandWithRemotes(t, remotes, "deps", "sync", "-p", root)
	require.NoError(t, err)

	manifestBytes, err := os.ReadFile(filepath.Join(root, deps.ManifestFileName))
	require.NoError(t, err)
	require.Equal(t, `manifest: {
  requires: {
    'github.com/x/core': 'v1.2.0'
  }
}
`, string(manifestBytes))
}

func TestDepsSyncRejectsInvalidGoLibrary(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: { 'github.com/x/bad': 'v1.0.0' }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource("imports: { bad: 'github.com/x/bad' }\n"), 0o644))
	badDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "go.mod"),
		[]byte("module github.com/x/bad\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "library.go"),
		[]byte("package bad\n\nfunc Library() any { return nil }\n"), 0o644))
	remotes := map[string]*resolve.Source{
		"github.com/x/bad@v1.0.0": {Commit: "c1", Path: badDir},
	}

	_, err := runCommandWithRemotes(t, remotes, "deps", "sync", "-p", root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must return *runtime.Library")
}

func TestDepsSyncAddsSentinelForExactReplacement(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "core")
	require.NoError(t, os.MkdirAll(local, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(local, "go.mod"),
		[]byte("module github.com/x/core\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(local, "lib.go"), validGoLibrarySource("core"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {}\nreplace: { 'github.com/x/core': './core' }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource("imports: { core: 'github.com/x/core' }\n"), 0o644))

	_, err := runCommand(t, "deps", "sync", "-p", root)
	require.NoError(t, err)

	manifestBytes, err := os.ReadFile(filepath.Join(root, deps.ManifestFileName))
	require.NoError(t, err)
	require.Equal(t, `manifest: {
  requires: {
    'github.com/x/core': 'v0.0.0-unobin-replaced'
  }
  replace: {
    'github.com/x/core': './core'
  }
}
`, string(manifestBytes))
	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.NotContains(t, lock.Deps, "github.com/x/core")
}

func TestDepsSyncRefusesGoModuleRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module example.com/app\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource("imports: {}\n"), 0o644))

	_, err := runCommand(t, "deps", "sync", "-p", root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "deps sync manages UB projects; use Go commands for Go modules")
}

func TestDepsSyncRejectsLocalGoImport(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "factory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource("imports: { aws: '../aws' }\n"), 0o644))

	awsDir := filepath.Join(base, "aws")
	require.NoError(t, os.MkdirAll(awsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "go.mod"),
		[]byte("module github.com/cloudboss/unobin-library-aws\n\ngo 1.26\n"), 0o644))

	_, err := runCommand(t, "deps", "sync", "-p", filepath.Join(root, "factory.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "is a Go library")
	require.Contains(t, err.Error(), "in manifest.ub:")
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

func TestDepsSync(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))
	// The floor lives in the manifest; sync rebuilds the lock from it.
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {\n  'github.com/x/core': 'v1.0.0'\n}\n"), 0o644))

	out, err := runCommandWithRemotes(t, goCoreRemotes(), "deps", "sync",
		"-p", filepath.Join(root, "factory.ub"))
	require.NoError(t, err)
	require.Contains(t, out, "Wrote manifest.ub")
	require.Contains(t, out, "lock.ub")

	_, err = os.Stat(filepath.Join(root, deps.SourceLockFileName))
	require.NoError(t, err)
	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, cliVersion(), lock.ToolchainVersion)
	require.Equal(t, map[string]*deps.LockedDep{
		"github.com/x/core": {Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "abc123"},
	}, lock.Deps)
}

func TestDepsSyncRootImportUsesRepoFloor(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource("imports: { core: 'github.com/x/core' }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {\n  'github.com/x/core': 'v1.0.0'\n}\n"), 0o644))

	out, err := runCommandWithRemotes(t, goCoreRemotes(), "deps", "sync",
		"-p", filepath.Join(root, "factory.ub"))
	require.NoError(t, err)
	require.Contains(t, out, "Wrote manifest.ub")

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, map[string]*deps.LockedDep{
		"github.com/x/core": {Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "abc123"},
	}, lock.Deps)
}

func TestDepsSyncUsesAncestorManifest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	child := filepath.Join(root, "services", "app")
	require.NoError(t, os.MkdirAll(child, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(child, "factory.ub"),
		factorySource("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {\n  'github.com/x/core': 'v1.0.0'\n}\n"), 0o644))

	_, err := runCommandWithRemotes(t, goCoreRemotes(), "deps", "sync",
		"-p", filepath.Join(child, "factory.ub"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(root, deps.SourceLockFileName))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(child, deps.SourceLockFileName))
	require.True(t, os.IsNotExist(err))
}

func TestDepsSyncResolvesLocalImportsFromFactoryDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	child := filepath.Join(root, "services", "app")
	require.NoError(t, os.MkdirAll(filepath.Join(child, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, "factory.ub"), []byte(`
factory: {
  imports: { local: './lib' }
  data: { message: local.message {} }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, "lib", "library.ub"), []byte(`
message: data {
  outputs: { text: { value: 'hi' } }
}
`), 0o644))

	_, err := runCommand(t, "deps", "sync", "-p", filepath.Join(child, "factory.ub"))
	require.NoError(t, err)

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Empty(t, lock.Deps)
}

func TestDepsSyncRejectsPositionalPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "ub", "project-b"), 0o755))
	t.Chdir(root)
	require.NoError(t, os.WriteFile(deps.ManifestFileName,
		[]byte("manifest: { requires: {} }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join("ub", "project-b", deps.ManifestFileName),
		[]byte("manifest: { requires: {} }\n"), 0o644))

	_, err := runCommand(t, "deps", "sync", "ub/project-b")
	require.Error(t, err)
	require.Contains(t, err.Error(), "accepts 0 arg(s), received 1")
}

func TestDepsSyncDefaultPathUsesFactoryUB(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	t.Chdir(root)
	require.NoError(t, os.WriteFile("factory.ub", []byte(`
factory: {
  imports: {
    core: 'github.com/x/core//lib'
  }
}
`), 0o644))
	require.NoError(t, os.WriteFile(deps.ManifestFileName,
		[]byte("manifest: { requires: { 'github.com/x/core': 'v1.0.0' } }\n"), 0o644))

	out, err := runCommandWithRemotes(t, goCoreRemotes(), "deps", "sync")
	require.NoError(t, err)
	require.Contains(t, out, "Wrote manifest.ub")
	require.Contains(t, out, "lock.ub")

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, cliVersion(), lock.ToolchainVersion)
	require.Equal(t, map[string]*deps.LockedDep{
		"github.com/x/core": {Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "abc123"},
	}, lock.Deps)
}

func TestDepsSyncAllowsSelfImport(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(`
factory: {
  imports: { self: '.' }
  data: { message: self.message {} }
  outputs: { text: { value: data.message.text } }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "library.ub"), []byte(`
message: data {
  outputs: { text: { value: 'hi' } }
}
`), 0o644))

	_, err := runCommand(t, "deps", "sync", "-p", filepath.Join(root, "factory.ub"))
	require.NoError(t, err)

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Empty(t, lock.Deps)
}

func TestDepsSyncManifest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(`
factory: {
  imports: {
    core: 'github.com/x/core//lib'
  }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		[]byte("manifest: { requires: { 'github.com/x/core': 'v1.0.0' } }\n"), 0o644))

	out, err := runCommandWithRemotes(t, goCoreRemotes(), "deps", "sync",
		"-p", filepath.Join(root, "factory.ub"))
	require.NoError(t, err)
	require.Contains(t, out, "Wrote manifest.ub")

	manifestBytes, err := os.ReadFile(filepath.Join(root, deps.ManifestFileName))
	require.NoError(t, err)
	require.Equal(t, `manifest: {
  requires: {
    'github.com/x/core': 'v1.0.0'
  }
}
`, string(manifestBytes))
}

func TestDepsSyncRootProjectOwnsPackageImport(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(`
factory: {
  imports: {
    helloer: 'github.com/scratch/repo//ub/helloer'
  }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		[]byte("manifest: { requires: { 'github.com/scratch/repo': 'v0.8.0' } }\n"), 0o644))
	projectFS := fstest.MapFS{
		deps.ManifestFileName: &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
		"ub/helloer/library.ub": &fstest.MapFile{Data: []byte(`
hello: resource {
  outputs: { message: { value: 'hi' } }
}
`)},
	}
	projectHash, err := deps.HashUBProject(projectFS)
	require.NoError(t, err)
	remotes := map[string]*resolve.Source{
		"github.com/scratch/repo//ub/helloer@v0.8.0": {
			Commit: "scratch",
			Hash:   "sha256:package",
			FS: fstest.MapFS{"library.ub": &fstest.MapFile{Data: []byte(`
hello: resource {
  outputs: { message: { value: 'hi' } }
}
`)}},
		},
		"github.com/scratch/repo@v0.8.0": {
			Commit: "scratch",
			Hash:   "sha256:project",
			FS:     projectFS,
		},
	}

	_, err = runCommandWithRemotes(t, remotes, "deps", "sync", "-p", root)
	require.NoError(t, err)

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, map[string]*deps.LockedDep{
		"github.com/scratch/repo": {
			Kind:    deps.LockKindUB,
			Version: "v0.8.0",
			Commit:  "scratch",
			Hash:    projectHash,
		},
	}, lock.Deps)
}

func TestDepsSyncReportsPackageMissingFromSelectedProject(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(`
factory: {
  imports: {
    comprehensions: 'github.com/scratch/repo//ub/project-b/comprehensions'
    helloer:        'github.com/scratch/repo//ub/helloer'
  }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		[]byte("manifest: { requires: { 'github.com/scratch/repo': 'v0.8.0' } }\n"), 0o644))
	remotes := map[string]*resolve.Source{
		"github.com/scratch/repo@v0.8.0": {
			Commit: "scratch",
			Hash:   "sha256:project",
			FS: fstest.MapFS{
				deps.ManifestFileName: &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
			},
		},
		"github.com/scratch/repo//ub/helloer@v0.8.0": {
			Commit: "scratch",
			Hash:   "sha256:package",
			FS: fstest.MapFS{"library.ub": &fstest.MapFile{Data: []byte(`
hello: data {
  outputs: { message: { value: 'hi' } }
}
`)}},
		},
		"github.com/scratch/repo//ub/project-b/comprehensions@v0.8.0": {
			Commit: "scratch",
			FS:     fstest.MapFS{},
		},
	}

	_, err := runCommandWithRemotes(t, remotes, "deps", "sync", "-p", root)

	require.Error(t, err)
	require.Contains(t, err.Error(), "selected project github.com/scratch/repo")
	require.Contains(t, err.Error(), "does not provide package")
	require.Contains(t, err.Error(), "add the owning project")
}

func TestDepsSyncRootAndNestedProjectOwnPackages(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(`
factory: {
  imports: {
    comprehensions: 'github.com/scratch/repo//ub/project-b/comprehensions'
    helloer:        'github.com/scratch/repo//ub/helloer'
  }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName), []byte(`manifest: {
  requires: {
    'github.com/scratch/repo':               'v0.8.0'
    'github.com/scratch/repo//ub/project-b': 'v0.1.0'
  }
}
`), 0o644))
	rootFS := fstest.MapFS{
		deps.ManifestFileName: &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
	}
	rootHash, err := deps.HashUBProject(rootFS)
	require.NoError(t, err)
	projectBFS := fstest.MapFS{
		deps.ManifestFileName: &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
	}
	projectBHash, err := deps.HashUBProject(projectBFS)
	require.NoError(t, err)
	remotes := map[string]*resolve.Source{
		"github.com/scratch/repo@v0.8.0": {
			Commit: "root",
			Hash:   "sha256:root",
			FS:     rootFS,
		},
		"github.com/scratch/repo//ub/helloer@v0.8.0": {
			Commit: "root",
			Hash:   "sha256:helloer",
			FS: fstest.MapFS{"library.ub": &fstest.MapFile{Data: []byte(`
hello: data {
  outputs: { message: { value: 'hi' } }
}
`)}},
		},
		"github.com/scratch/repo//ub/project-b@ub/project-b/v0.1.0": {
			Commit: "project-b",
			Hash:   "sha256:project-b",
			FS:     projectBFS,
		},
		"github.com/scratch/repo//ub/project-b/comprehensions@ub/project-b/v0.1.0": {
			Commit: "project-b",
			Hash:   "sha256:comprehensions",
			FS: fstest.MapFS{"library.ub": &fstest.MapFile{Data: []byte(`
list: data {
  outputs: { value: { value: [] } }
}
`)}},
		},
	}

	_, err = runCommandWithRemotes(t, remotes, "deps", "sync", "-p", root)
	require.NoError(t, err)

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, map[string]*deps.LockedDep{
		"github.com/scratch/repo": {
			Kind:    deps.LockKindUB,
			Version: "v0.8.0",
			Commit:  "root",
			Hash:    rootHash,
		},
		"github.com/scratch/repo//ub/project-b": {
			Kind:    deps.LockKindUB,
			Version: "v0.1.0",
			Commit:  "project-b",
			Hash:    projectBHash,
		},
	}, lock.Deps)
}

func TestDepsSyncLibraryProject(t *testing.T) {
	// A library project: body files, no factory.ub. Its dependencies are
	// managed the same way a factory's are.
	root := filepath.Join(t.TempDir(), "greeter")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "library.ub"), []byte(`
greeting: resource {
  imports: { core: 'github.com/x/core//lib' }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {\n  'github.com/x/core': 'v1.0.0'\n}\n"), 0o644))

	_, err := runCommandWithRemotes(t, goCoreRemotes(), "deps", "sync", "-p", root)
	require.NoError(t, err)

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, map[string]*deps.LockedDep{
		"github.com/x/core": {Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "abc123"},
	}, lock.Deps)
}

func TestDepsSyncSkipsNestedProjects(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	child := filepath.Join(root, "library-c")
	require.NoError(t, os.MkdirAll(child, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(`
factory: {
  imports: { shared: 'example.com/shared//lib' }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: { 'example.com/shared': 'v1.0.0' }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, deps.ManifestFileName),
		manifestSource("requires: { 'example.com/nested': 'v1.0.0' }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, "abc.ub"), []byte(`
thing: resource {
  imports: { nested: 'example.com/nested//lib' }
}
`), 0o644))
	remotes := map[string]*resolve.Source{
		"example.com/shared@v1.0.0": {
			Commit: "shared",
			FS: fstest.MapFS{
				"go.mod": &fstest.MapFile{Data: []byte("module example.com/shared\n")},
			},
		},
		"example.com/shared//lib@v1.0.0": {
			Commit: "shared",
			FS: fstest.MapFS{
				"go.mod": &fstest.MapFile{Data: []byte("module example.com/shared/lib\n")},
				"lib.go": &fstest.MapFile{Data: []byte("package lib")},
			},
		},
	}

	_, err := runCommandWithRemotes(t, remotes, "deps", "sync", "-p", root)
	require.NoError(t, err)

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, map[string]*deps.LockedDep{
		"example.com/shared": {
			Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "shared",
		},
	}, lock.Deps)
	require.NoFileExists(t, filepath.Join(child, deps.SourceLockFileName))
}

func TestDepsSyncNestedProjectFromPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	child := filepath.Join(root, "library-c")
	require.NoError(t, os.MkdirAll(child, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(`
factory: {
  imports: { root: 'example.com/root//lib' }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, deps.ManifestFileName),
		manifestSource("requires: { 'example.com/nested': 'v1.0.0' }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, "abc.ub"), []byte(`
thing: resource {
  imports: { nested: 'example.com/nested//lib' }
}
`), 0o644))
	remotes := map[string]*resolve.Source{
		"example.com/nested@v1.0.0": {
			Commit: "nested",
			FS: fstest.MapFS{
				"go.mod": &fstest.MapFile{Data: []byte("module example.com/nested\n")},
			},
		},
		"example.com/nested//lib@v1.0.0": {
			Commit: "nested",
			FS: fstest.MapFS{
				"go.mod": &fstest.MapFile{Data: []byte("module example.com/nested/lib\n")},
				"lib.go": &fstest.MapFile{Data: []byte("package lib")},
			},
		},
	}

	_, err := runCommandWithRemotes(t, remotes, "deps", "sync", "-p", child)
	require.NoError(t, err)

	lock, err := deps.ReadLock(os.DirFS(child))
	require.NoError(t, err)
	require.Equal(t, map[string]*deps.LockedDep{
		"example.com/nested": {
			Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "nested",
		},
	}, lock.Deps)
}

func TestDepsSyncWithReplace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "factory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource("imports: { aws: 'github.com/cloudboss/unobin-library-aws' }\n"), 0o644))

	// A local Go library the manifest replaces in (no remote, no floor).
	awsDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "go.mod"),
		[]byte("module github.com/cloudboss/unobin-library-aws\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(awsDir, "library.go"), validGoLibrarySource("aws"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {}\nreplace: { 'github.com/cloudboss/unobin-library-aws': '"+
			awsDir+"' }\n"), 0o644))

	_, err := runCommand(t, "deps", "sync", "-p", filepath.Join(root, "factory.ub"))
	require.NoError(t, err)

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Empty(t, lock.Deps, "a replaced dependency is not locked")
}

func TestDepsSyncRejectsMissingFloor(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))

	// No manifest, so the imported repo has no floor.
	_, err := runCommand(t, "deps", "sync", "-p", filepath.Join(root, "factory.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "github.com/x/core")
	require.Contains(t, err.Error(), "deps get")
}

func TestDepsSyncPrunesStaleFloor(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))
	// gone/repo is listed but no longer imported; sync must remove it.
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {\n  'github.com/gone/repo': 'v1.0.0'\n"+
			"  'github.com/x/core': 'v1.0.0'\n}\n"), 0o644))

	_, err := runCommandWithRemotes(t, goCoreRemotes(), "deps", "sync",
		"-p", filepath.Join(root, "factory.ub"))
	require.NoError(t, err)

	manifestBytes, err := os.ReadFile(filepath.Join(root, deps.ManifestFileName))
	require.NoError(t, err)
	require.Equal(t, `manifest: {
  requires: {
    'github.com/x/core': 'v1.0.0'
  }
}
`, string(manifestBytes))
}

func writeProjectLock(t *testing.T, root string) fstest.MapFS {
	t.Helper()
	require.NoError(t, os.MkdirAll(root, 0o755))
	ubFS := fstest.MapFS{
		deps.ManifestFileName: &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
		"library.ub":          &fstest.MapFile{Data: []byte("hello: resource {}\n")},
	}
	ubHash, err := deps.HashUBProject(ubFS)
	require.NoError(t, err)
	lock := deps.NewLock()
	lock.ToolchainVersion = "dev"
	lock.Deps["github.com/x/core//lib"] = &deps.LockedDep{
		Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "c1",
	}
	lock.Deps["github.com/x/hello//ub"] = &deps.LockedDep{
		Kind: deps.LockKindUB, Version: "v2.0.0", Commit: "c2", Hash: ubHash,
	}
	require.NoError(t, deps.WriteSourceLock(filepath.Join(root, deps.SourceLockFileName), lock))
	return ubFS
}

func TestDepsList(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	writeProjectLock(t, root)
	out, err := runCommand(t, "deps", "list", "-p", filepath.Join(root, "factory.ub"))
	require.NoError(t, err)
	require.Equal(t,
		"github.com/x/core//lib v1.0.0 (go)\ngithub.com/x/hello//ub v2.0.0 (ub)\n", out)
}

func TestDepsListUsesAncestorManifest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	child := filepath.Join(root, "services", "app")
	require.NoError(t, os.MkdirAll(child, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		[]byte("manifest: { requires: {} }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, "factory.ub"),
		factorySource("description: 'x'\n"), 0o644))
	writeProjectLock(t, root)

	out, err := runCommand(t, "deps", "list", "-p", filepath.Join(child, "factory.ub"))
	require.NoError(t, err)
	require.Equal(t,
		"github.com/x/core//lib v1.0.0 (go)\ngithub.com/x/hello//ub v2.0.0 (ub)\n", out)
}

func TestDepsListAcceptsDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	writeProjectLock(t, root)
	want := "github.com/x/core//lib v1.0.0 (go)\ngithub.com/x/hello//ub v2.0.0 (ub)\n"

	out, err := runCommand(t, "deps", "list", "-p", root)
	require.NoError(t, err)
	require.Equal(t, want, out)

	out, err = runCommand(t, "deps", "list", "-p", root+string(filepath.Separator))
	require.NoError(t, err)
	require.Equal(t, want, out)
}

func TestDepsListNoLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(root, 0o755))
	_, err := runCommand(t, "deps", "list", "-p", filepath.Join(root, "factory.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "run `unobin deps sync`")
}

func TestDepsVerifyMatches(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	ubFS := writeProjectLock(t, root)
	remotes := map[string]*resolve.Source{
		"github.com/x/hello//ub@c2": {FS: ubFS},
	}
	out, err := runCommandWithRemotes(t, remotes, "deps", "verify",
		"-p", filepath.Join(root, "factory.ub"))
	require.NoError(t, err)
	require.Contains(t, out, "all dependencies verified")
}

func TestDepsVerifyDetectsMismatch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	writeProjectLock(t, root)
	remotes := map[string]*resolve.Source{
		"github.com/x/hello//ub@c2": {
			FS: fstest.MapFS{
				deps.ManifestFileName: &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
				"library.ub":          &fstest.MapFile{Data: []byte("changed: resource {}\n")},
			},
		},
	}
	_, err := runCommandWithRemotes(t, remotes, "deps", "verify",
		"-p", filepath.Join(root, "factory.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "hash mismatch")
}

func TestDepsClean(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	imports := filepath.Join(cache, "unobin", "imports", "github.com", "x", "y")
	require.NoError(t, os.MkdirAll(imports, 0o755))

	out, err := runCommand(t, "deps", "clean")
	require.NoError(t, err)
	require.Contains(t, out, "Removed the import cache")
	_, statErr := os.Stat(filepath.Join(cache, "unobin", "imports"))
	require.True(t, os.IsNotExist(statErr))
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

func TestDepsGetExactVersion(t *testing.T) {
	root := writeGetProject(t)
	out, err := runCommandWithRemotes(t, goLibRemotes("v1.2.0", "c12"),
		"deps", "get", "github.com/x/core//lib@v1.2.0", "-p", filepath.Join(root, "factory.ub"))
	require.NoError(t, err)
	require.Contains(t, out, "github.com/x/core//lib v1.2.0")

	manifestBytes, err := os.ReadFile(filepath.Join(root, deps.ManifestFileName))
	require.NoError(t, err)
	require.Equal(t, `manifest: {
  requires: {
    'github.com/x/core//lib': 'v1.2.0'
  }
}
`, string(manifestBytes))

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, "v1.2.0", lock.Deps["github.com/x/core//lib"].Version)
}

func TestDepsGetLatest(t *testing.T) {
	root := writeGetProject(t)
	_, err := runCommandWithRemotes(t, goLibRemotes("v2.0.0", "c20"),
		"deps", "get", "github.com/x/core//lib", "-p", filepath.Join(root, "factory.ub"))
	require.NoError(t, err)
	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", lock.Deps["github.com/x/core//lib"].Version)
}

func TestDepsGetRejectsReplacementSentinel(t *testing.T) {
	root := writeGetProject(t)
	calledTags := false
	prev := depsListTags
	depsListTags = func(string) ([]string, error) {
		calledTags = true
		return nil, fmt.Errorf("tags should not be listed")
	}
	t.Cleanup(func() { depsListTags = prev })

	_, err := runCommand(t, "deps", "get",
		"github.com/x/core@"+deps.ReplacementSentinel, "-p", root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "v0.0.0-unobin-replaced is reserved")
	require.False(t, calledTags)
}

func TestDepsGetRejectsPackageWithoutProjectMarker(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
		factorySource("imports: { helloer: 'github.com/x/libs//ub/helloer' }\n"), 0o644))
	stubListTags(t, map[string][]string{
		"github.com/x/libs": {"v0.8.0", "ub/helloer/v0.8.0"},
	})
	remotes := map[string]*resolve.Source{
		"github.com/x/libs//ub/helloer@v0.8.0": {
			Commit: "c1",
			FS: fstest.MapFS{
				"resource-hello.ub": &fstest.MapFile{Data: []byte(`
hello: resource {
  outputs: { message: { value: 'hi' } }
}
`)},
			},
		},
		"github.com/x/libs@v0.8.0": {
			Commit: "c1",
			FS: fstest.MapFS{
				deps.ManifestFileName: &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
			},
		},
	}

	_, err := runCommandWithRemotes(t, remotes,
		"deps", "get", "github.com/x/libs//ub/helloer", "-p", root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no manifest.ub or go.mod")
	require.Contains(t, err.Error(), "deps get operates on projects")
}

func TestDepsGetAcceptsNestedProject(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(`
factory: {
  imports: { comprehensions: 'github.com/x/libs//ub/project-b/comprehensions' }
}
`), 0o644))
	stubListTags(t, map[string][]string{"github.com/x/libs": {"ub/project-b/v0.1.0"}})
	remotes := map[string]*resolve.Source{
		"github.com/x/libs//ub/project-b@ub/project-b/v0.1.0": {
			Commit: "project-b",
			Hash:   "sha256:project",
			FS: fstest.MapFS{
				deps.ManifestFileName: &fstest.MapFile{Data: []byte("manifest: { requires: {} }\n")},
				"comprehensions/library.ub": &fstest.MapFile{Data: []byte(`
hello: resource {
  outputs: { message: { value: 'hi' } }
}
`)},
			},
		},
		"github.com/x/libs//ub/project-b/comprehensions@ub/project-b/v0.1.0": {
			Commit: "project-b",
			Hash:   "sha256:package",
			FS: fstest.MapFS{"library.ub": &fstest.MapFile{Data: []byte(`
hello: resource {
  outputs: { message: { value: 'hi' } }
}
`)}},
		},
	}

	_, err := runCommandWithRemotes(t, remotes,
		"deps", "get", "github.com/x/libs//ub/project-b", "-p", root)
	require.NoError(t, err)

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, "v0.1.0", lock.Deps["github.com/x/libs//ub/project-b"].Version)
}

func TestCompileToStdout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	src := `
imports: { core: 'github.com/cloudboss/unobin/pkg/libraries/core' }
actions: { hi: core.command { argv: ['echo', 'hi'] } }
`
	require.NoError(t, os.WriteFile(stackPath, factorySource(src), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin/pkg/libraries/core": "v0.1.0",
	})

	out, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.NoError(t, err)

	require.Contains(t, out, "package main")
	require.Contains(t, out, `factoryName        = "demo-factory"`)
	require.Contains(t, out,
		"var (\n\tfactoryVersion  string\n\tcontentRevision string\n\tunobinVersion   string\n)")
	require.Contains(t, out, `"github.com/cloudboss/unobin/pkg/libraries/core"`)
}

func TestCompileUsesAncestorLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo-factory")
	child := filepath.Join(root, "services", "app")
	require.NoError(t, os.MkdirAll(child, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		[]byte("manifest: { requires: {} }\n"), 0o644))
	stackPath := filepath.Join(child, "factory.ub")
	src := `
factory: {
  imports: { core: 'github.com/cloudboss/unobin/pkg/libraries/core' }
  actions: { hi: core.command { argv: ['echo', 'hi'] } }
}
`
	require.NoError(t, os.WriteFile(stackPath, factorySource(src), 0o644))
	writeCompileLock(t, root, map[string]string{
		"github.com/cloudboss/unobin/pkg/libraries/core": "v0.1.0",
	})

	out, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.NoError(t, err)
	require.Contains(t, out, "package main")
}

func TestCompileRejectsLocalImportIntoNestedProject(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo-factory")
	child := filepath.Join(root, "library-c")
	require.NoError(t, os.MkdirAll(child, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(`
factory: {
  imports: { child: './library-c' }
  data: { message: child.message {} }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, deps.ManifestFileName),
		manifestSource("requires: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, "library.ub"), []byte(`
message: data {
  outputs: { text: { value: 'hi' } }
}
`), 0o644))

	_, err := runCommand(t, "compile", "-p", filepath.Join(root, "factory.ub"), "-o", "-")
	require.Error(t, err)
	require.Contains(t, err.Error(), "different project")
	require.Contains(t, err.Error(), "manifest.replace")
}

func TestCompileResolvesLocalImportsFromFactoryDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo-factory")
	child := filepath.Join(root, "services", "app")
	require.NoError(t, os.MkdirAll(filepath.Join(child, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, "factory.ub"), []byte(`
factory: {
  imports: { local: './lib' }
  data: { message: local.message {} }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, "lib", "library.ub"), []byte(`
message: data {
  outputs: { text: { value: 'hi' } }
}
`), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(child, "factory.ub"), "-o", outDir)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(outDir, "internal", "local", "local.go"))
}

func TestCompileSourceDeclaredFactoryToStdout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	src := `
factory: {
  imports: { core: 'github.com/cloudboss/unobin/pkg/libraries/core' }
  actions: { hi: core.command { argv: ['echo', 'hi'] } }
  outputs: { stdout: { value: action.hi.stdout } }
}
`
	require.NoError(t, os.WriteFile(stackPath, factorySource(src), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin/pkg/libraries/core": "v0.1.0",
	})

	out, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.NoError(t, err)

	require.Contains(t, out, "package main")
	require.Contains(t, out, `hi: core.command`)
	require.Contains(t, out, "factory: {")
}

func TestCompileDirectoryUsesFactoryUB(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	src := `
factory: {
  imports: { core: 'github.com/cloudboss/unobin/pkg/libraries/core' }
  actions: { hi: core.command { argv: ['echo', 'hi'] } }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"), []byte(src), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin/pkg/libraries/core": "v0.1.0",
	})

	out, err := runCommand(t, "compile", "-p", dir, "-o", "-")
	require.NoError(t, err)

	require.Contains(t, out, "package main")
	require.Contains(t, out, `factoryName        = "demo-factory"`)
	require.Contains(t, out, `hi: core.command`)
}

func TestCompileDefaultPathUsesFactoryUB(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	t.Chdir(dir)
	src := `
factory: {
  imports: { core: 'github.com/cloudboss/unobin/pkg/libraries/core' }
  actions: { hi: core.command { argv: ['echo', 'hi'] } }
}
`
	require.NoError(t, os.WriteFile("factory.ub", []byte(src), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin/pkg/libraries/core": "v0.1.0",
	})

	out, err := runCommand(t, "compile", "-o", "-")
	require.NoError(t, err)

	require.Contains(t, out, "package main")
	require.Contains(t, out, `factoryName        = "demo-factory"`)
	require.Contains(t, out, `hi: core.command`)
}

// TestCompileWriteOut compiles to an output directory and checks the
// files written there. The import deliberately lives under the unobin
// module path: such a library is served by the unobin require the
// generated go.mod always pins, with no require line of its own.
func TestCompileWriteOut(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	src := `
imports: {
  core: 'github.com/cloudboss/unobin/pkg/libraries/core'
}
`
	require.NoError(t, os.WriteFile(stackPath, factorySource(src), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin/pkg/libraries/core": "v0.1.0",
	})

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", stackPath, "-o", outDir)
	require.NoError(t, err)

	mainBytes, err := os.ReadFile(filepath.Join(outDir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(mainBytes), "package main")

	goModBytes, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(goModBytes), "module demo-factory")
	require.Contains(t, string(goModBytes), "github.com/cloudboss/unobin v0.1.0")
	require.NotContains(t, string(goModBytes), "github.com/cloudboss/unobin/pkg/libraries/core")
}

// TestCompileUsesLockVersion compiles a factory whose import has no
// @version: the version must come from lock.ub. The fake resolver only
// serves the locked version, and the generated go.mod must record it.
func TestCompileUsesLockVersion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
		factorySource("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))
	lock := deps.NewLock()
	lock.ToolchainVersion = "dev"
	lock.Deps["github.com/x/core//lib"] = &deps.LockedDep{
		Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "c1",
	}
	require.NoError(t, deps.WriteSourceLock(filepath.Join(dir, deps.SourceLockFileName), lock))

	remotes := map[string]*resolve.Source{
		"github.com/x/core//lib@v1.0.0": {
			FS: fstest.MapFS{"lib.go": &fstest.MapFile{Data: []byte("package lib")}},
		},
	}
	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommandWithRemotes(t, remotes, "compile",
		"-p", filepath.Join(dir, "factory.ub"), "-o", outDir)
	require.NoError(t, err)
	goMod, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(goMod), "github.com/x/core/lib v1.0.0")
}

func TestCompileRequiresGoModuleForSubpackage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
		factorySource("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))
	lock := deps.NewLock()
	lock.ToolchainVersion = "dev"
	lock.Deps["github.com/x/core"] = &deps.LockedDep{
		Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "c1",
	}
	require.NoError(t, deps.WriteSourceLock(filepath.Join(dir, deps.SourceLockFileName), lock))

	moduleDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(moduleDir, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"),
		[]byte("module github.com/x/core\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "lib", "library.go"),
		validGoLibrarySource("lib"), 0o644))

	remotes := map[string]*resolve.Source{
		"github.com/x/core//lib@v1.0.0": {
			Path:           filepath.Join(moduleDir, "lib"),
			ProjectPath:    moduleDir,
			ModuleRootPath: moduleDir,
			ModulePath:     "github.com/x/core",
			GoImportPath:   "github.com/x/core/lib",
		},
	}
	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommandWithRemotes(t, remotes, "compile",
		"-p", filepath.Join(dir, "factory.ub"), "-o", outDir)
	require.NoError(t, err)
	goMod, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	want := "module demo-factory\n\n" +
		"go " + compilepkg.GoMajorMinor() + "\n\n" +
		"require (\n" +
		"\tgithub.com/cloudboss/unobin v0.1.0\n" +
		"\tgithub.com/x/core v1.0.0\n" +
		")\n"
	require.Equal(t, want, string(goMod))
}

// TestCompileRequiresLockedVersion compiles a factory whose import is
// versionless and has no lock. Compile never selects a version on its own,
// so it must fail and point at deps sync.
// TestCompileWithReplacedGoLibrary compiles a factory that imports a Go
// library by URL while the manifest replaces it with a local checkout. The
// import needs no locked version, and the generated go.mod requires the
// module and replaces it with the local path.
func TestCompileWithReplacedGoLibrary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
		factorySource("imports: { aws: 'github.com/cloudboss/unobin-library-aws' }\n"), 0o644))

	awsDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "go.mod"),
		[]byte("module github.com/cloudboss/unobin-library-aws\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(awsDir, "library.go"), validGoLibrarySource("aws"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ManifestFileName),
		manifestSource("requires: {}\nreplace: { 'github.com/cloudboss/unobin-library-aws': '"+
			awsDir+"' }\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "factory.ub"),
		"-o", outDir)
	require.NoError(t, err)

	goMod, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(goMod), "github.com/cloudboss/unobin-library-aws v0.0.0")
	require.Contains(t, string(goMod),
		"github.com/cloudboss/unobin-library-aws => "+awsDir)
}

func TestCompileRequiresLockedVersion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
		factorySource("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "factory.ub"), "-o", outDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "github.com/x/core")
	require.Contains(t, err.Error(), "deps sync")
}

// setCLIVersion stamps the CLI version for one test, the way a release
// build's ldflags would.
func setCLIVersion(t *testing.T, v string) {
	t.Helper()
	prev := Version
	Version = v
	t.Cleanup(func() { Version = prev })
}

// TestCompilePinsGoModToCLIVersion proves the generated go.mod requires
// unobin at the version of the compiling CLI itself, so the runtime a
// factory links is the one its compile checks ran with.
func TestCompilePinsGoModToCLIVersion(t *testing.T) {
	setCLIVersion(t, "v9.9.9")
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
		factorySource("description: 'minimal'\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "factory.ub"), "-o", outDir)
	require.NoError(t, err)

	goMod, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(goMod), "github.com/cloudboss/unobin v9.9.9")
}

// TestCompileDevVersionNeedsReplace proves a development CLI, which has
// no release version to pin, refuses to compile unless the unobin repo
// is replaced.
func TestCompileDevVersionNeedsReplace(t *testing.T) {
	setCLIVersion(t, "dev")
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
		factorySource("description: 'minimal'\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "factory.ub"), "-o", outDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--replace-unobin")
	require.Contains(t, err.Error(), "manifest.ub")
}

// TestCompileDevVersionAcceptsManifestReplace proves the manifest's
// replace of the unobin repo satisfies the development gate the same
// way --replace-unobin does, and appears in the generated go.mod.
func TestCompileDevVersionAcceptsManifestReplace(t *testing.T) {
	setCLIVersion(t, "dev")
	rootDir := findUnobinRoot(t)
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
		factorySource("description: 'minimal'\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ManifestFileName),
		manifestSource("requires: {}\nreplace: { 'github.com/cloudboss/unobin': '"+rootDir+"' }\n"),
		0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "factory.ub"), "-o", outDir)
	require.NoError(t, err)

	goMod, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(goMod), "github.com/cloudboss/unobin v0.0.0")
	require.Contains(t, string(goMod), "github.com/cloudboss/unobin => "+rootDir)
}

func TestCompileReplaceUnobinDoesNotNeedLock(t *testing.T) {
	rootDir := findUnobinRoot(t)
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"), []byte(`
factory: {
  imports: { greet: 'github.com/cloudboss/unobin//examples/configurations/greet' }
  actions: { say: greet.say { message: 'world' } }
}
`), 0o644))

	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "factory.ub"),
		"-o", "-", "--replace-unobin", rootDir)
	require.NoError(t, err)
}

func TestCompileReplaceGoModuleDoesNotNeedLock(t *testing.T) {
	rootDir := findUnobinRoot(t)
	libDir := filepath.Join(rootDir, "examples", "configurations", "greet")
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"), []byte(`
factory: {
  imports: { greet: 'github.com/x/greet' }
  actions: { say: greet.say { message: 'world' } }
}
`), 0o644))

	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "factory.ub"),
		"-o", "-", "--replace-go-module", "github.com/x/greet="+libDir)
	require.NoError(t, err)
}

// TestCompileManifestToolchainLine proves the manifest's unobin-version
// line pins which CLI may compile the project: a match proceeds, a mismatch
// stops with the version to install, and a replaced unobin proceeds
// with a notice since the replacement runs regardless.
func TestCompileManifestToolchainLine(t *testing.T) {
	write := func(t *testing.T, manifest string) string {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "demo-factory")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
			factorySource("description: 'minimal'\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ManifestFileName),
			manifestSource(manifest), 0o644))
		return dir
	}

	t.Run("matching line proceeds", func(t *testing.T) {
		dir := write(t, "unobin-version: 'v0.1.0'\nrequires: {}\n")
		outDir := filepath.Join(t.TempDir(), "build")
		_, err := runCommand(t, "compile",
			"-p", filepath.Join(dir, "factory.ub"), "-o", outDir)
		require.NoError(t, err)
	})

	t.Run("mismatched line is refused", func(t *testing.T) {
		dir := write(t, "unobin-version: 'v0.9.9'\nrequires: {}\n")
		outDir := filepath.Join(t.TempDir(), "build")
		_, err := runCommand(t, "compile",
			"-p", filepath.Join(dir, "factory.ub"), "-o", outDir)
		require.Error(t, err)
		require.Contains(t, err.Error(), "pins unobin v0.9.9")
		require.Contains(t, err.Error(), "v0.1.0")
	})

	t.Run("replaced unobin proceeds with a notice", func(t *testing.T) {
		rootDir := findUnobinRoot(t)
		dir := write(t, "unobin-version: 'v0.9.9'\nrequires: {}\n"+
			"replace: { 'github.com/cloudboss/unobin': '"+rootDir+"' }\n")
		outDir := filepath.Join(t.TempDir(), "build")
		out, err := runCommand(t, "compile",
			"-p", filepath.Join(dir, "factory.ub"), "-o", outDir)
		require.NoError(t, err)
		require.Contains(t, out, "notice:")
		require.Contains(t, out, "v0.9.9")
	})
}

// TestCompileOfflineLocalLibraries proves the local-testing workflow
// under version pinning: a development CLI, the unobin repo replaced in
// the manifest, a UB library imported by relative path, and a Go
// library imported by module path and replaced to a local directory.
// Nothing is fetched, and the generated go.mod replaces both modules.
func TestCompileOfflineLocalLibraries(t *testing.T) {
	setCLIVersion(t, "dev")
	rootDir := findUnobinRoot(t)
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"), factorySource(`
imports: {
  net: './libraries/net'
  aws: 'github.com/cloudboss/unobin-library-aws'
}
`), 0o644))

	netDir := filepath.Join(dir, "libraries", "net")
	require.NoError(t, os.MkdirAll(netDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(netDir, "library.ub"), []byte(`
cluster: resource {
  description: 'a cluster'
  resources: { x: local.file { path: '/tmp/x', content: 'hi', mode: 420 } }
}
`), 0o644))

	awsDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "go.mod"),
		[]byte("module github.com/cloudboss/unobin-library-aws\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(awsDir, "library.go"), validGoLibrarySource("aws"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ManifestFileName),
		manifestSource("requires: {}\nreplace: {\n"+
			"  'github.com/cloudboss/unobin': '"+rootDir+"'\n"+
			"  'github.com/cloudboss/unobin-library-aws': '"+awsDir+"'\n"+
			"}\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "factory.ub"), "-o", outDir)
	require.NoError(t, err)

	goMod, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(goMod), "github.com/cloudboss/unobin v0.0.0")
	require.Contains(t, string(goMod), "github.com/cloudboss/unobin => "+rootDir)
	require.Contains(t, string(goMod), "github.com/cloudboss/unobin-library-aws => "+awsDir)
	require.FileExists(t, filepath.Join(outDir, "internal", "net", "net.go"))
}

// TestCompileRefusesUnreplacedUnobinImport proves an import from the
// unobin repository must be served by a replace: its source version
// may not float free of the toolchain that compiles against it.
func TestCompileRefusesUnreplacedUnobinImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"),
		factorySource("imports: { x: 'github.com/cloudboss/unobin//examples/thing' }\n"), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin": "v0.5.0",
	})

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "factory.ub"), "-o", outDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolchain-versioned")
	require.Contains(t, err.Error(), "replace")
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

func TestCompileRequiresOut(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource("description: 'x'"), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out")
}

func TestCompileMissingStackFile(t *testing.T) {
	_, err := runCommand(t, "compile", "-p", "/no/such/path/factory.ub", "-o", "-")
	require.Error(t, err)
}

func TestCompileInvalidStackFails(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource("exports: { x: 'y.ub' }\n"), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
}

func TestCompileInvalidReferenceFails(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
resources: { bad: local.file { path: var.missing } }
`), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown input "missing"`)
}

func TestCompileUnimportedResourceModuleFails(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports:   { std: 'github.com/cloudboss/unobin-library-std' }
resources: { welcome: greeter.greeting { message: 'hello' } }
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin-library-std": "v0.1.0",
	})

	_, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
	require.Contains(t, err.Error(), `library "greeter" is not imported`)
}

func TestCompileUnknownTrailingFieldFails(t *testing.T) {
	goModDir := writeFakeGoModule(t)

	dir := t.TempDir()
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports:   { fake: 'example.com/fake' }
resources: { x: fake.thing {} }
outputs:   { bad: { value: resource.x.nonexistent } }
`), 0o644))
	writeCompileLock(t, dir, map[string]string{"example.com/fake": "v0.1.0"})

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
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports:   { fake: 'example.com/fake' }
resources: { x: fake.thing {} }
outputs:   { good: { value: resource.x.id } }
`), 0o644))
	writeCompileLock(t, dir, map[string]string{"example.com/fake": "v0.1.0"})

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
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports:   { partial: 'example.com/partial' }
resources: { x: partial.thing {} }
`), 0o644))
	writeCompileLock(t, dir, map[string]string{"example.com/partial": "v0.1.0"})

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
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports: {
  broken: 'example.com/broken'
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{"example.com/broken": "v0.1.0"})

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
			Hash:   "sha256:fakehash",
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

func TestCompileReplaceUnobinUBSubdir(t *testing.T) {
	fakeUnobin := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(fakeUnobin, "some-lib"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(fakeUnobin, "some-lib", "library.ub"), []byte(`
foo: resource {
  description: 'a foo'
  resources: { x: local.file { path: '/tmp/x', content: 'hi', mode: 420 } }
}
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports: {
  some: 'github.com/cloudboss/unobin//some-lib'
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin//some-lib": "v0.1.0",
	})

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile",
		"-p", stackPath, "-o", outDir,
		"--replace-unobin", fakeUnobin)
	require.NoError(t, err)

	pkgBytes, err := os.ReadFile(filepath.Join(outDir, "internal", "some", "some.go"))
	require.NoError(t, err)
	require.Contains(t, string(pkgBytes), "package some")
	require.Regexp(t, `"foo":\s*\{\s*Name:\s*"foo"`, string(pkgBytes))
}

func TestCompileReplaceUnobinGoSubdirRejectsInvalidLibrary(t *testing.T) {
	fakeUnobin := t.TempDir()
	goModDir := filepath.Join(fakeUnobin, "pkg/libraries/local")
	require.NoError(t, os.MkdirAll(goModDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(goModDir, "go.mod"),
		[]byte("module github.com/cloudboss/unobin/pkg/libraries/local\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(goModDir, "library.go"),
		[]byte("package local\n\nfunc Library() any { return nil }\n"), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports: {
  local: 'github.com/cloudboss/unobin//pkg/libraries/local'
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin//pkg/libraries/local": "v0.1.0",
	})

	_, err := runCommand(t, "compile",
		"-p", stackPath, "-o", "-",
		"--version", "v0.1.0",
		"--replace-unobin", fakeUnobin)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must return *runtime.Library")
}

func TestCompileReplaceUnobinMissingPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports: {
  local: 'github.com/cloudboss/unobin//pkg/libraries/local'
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin": "v0.1.0",
	})

	_, err := runCommand(t, "compile",
		"-p", stackPath, "-o", "-",
		"--replace-unobin", filepath.Join(t.TempDir(), "no-such-tree"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "replace github.com/cloudboss/unobin")
}

func TestCompileWithRemoteGoSubpath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(`
imports: {
  std: 'github.com/cloudboss/unobin-library-std//fs'
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin-library-std": "v0.1.0",
	})
	addCommandRemoteSource(t, "github.com/cloudboss/unobin-library-std//fs@v0.1.0",
		goTestSource("github.com/cloudboss/unobin-library-std/fs"))

	out, err := runCommand(t, "compile", "-p", stackPath, "-o", "-",
		"--version", "v0.1.0")
	require.NoError(t, err)
	require.Contains(t, out, `"github.com/cloudboss/unobin-library-std/fs"`)
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
