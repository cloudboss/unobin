package root

import (
	"bytes"
	"fmt"
	"io/fs"
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
	key := ri.URL + "@" + ri.Version
	if ri.Subdir != "" {
		key = ri.URL + "//" + ri.Subdir + "@" + ri.Version
	}
	if src, found := r.remotes[key]; found {
		return src, nil
	}
	// A non-nil empty FS so callers that inspect it (a manifest read, a
	// UB-library check) see an empty Go-library source rather than nil.
	return &resolve.Source{Commit: "fakecommit", FS: fstest.MapFS{}}, nil
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

func TestDepsSyncRejectsLocalGoImport(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "factory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.ub"),
		[]byte("imports: { aws: '../aws' }\n"), 0o644))

	awsDir := filepath.Join(base, "aws")
	require.NoError(t, os.MkdirAll(awsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "go.mod"),
		[]byte("module github.com/cloudboss/unobin-library-aws\n\ngo 1.26\n"), 0o644))

	_, err := runCommand(t, "deps", "sync", "-p", filepath.Join(root, "main.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "is a Go library")
	require.Contains(t, err.Error(), "in unobin.manifest:")
}

func goCoreRemotes() map[string]*resolve.Source {
	return map[string]*resolve.Source{
		// the repo root, read by the version walk: no manifest, so a leaf.
		"github.com/x/core@v1.0.0": {FS: fstest.MapFS{}},
		// the imported library, pinned in the lock as a Go library.
		"github.com/x/core//lib@v1.0.0": {
			Commit: "abc123",
			FS:     fstest.MapFS{"lib.go": &fstest.MapFile{Data: []byte("package lib")}},
		},
	}
}

func TestDepsSync(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.ub"),
		[]byte("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))
	// The floor lives in the manifest; sync rebuilds the lock from it.
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		[]byte("requires: {\n  'github.com/x/core': 'v1.0.0'\n}\n"), 0o644))

	out, err := runCommandWithRemotes(t, goCoreRemotes(), "deps", "sync",
		"-p", filepath.Join(root, "main.ub"))
	require.NoError(t, err)
	require.Contains(t, out, "Wrote unobin.manifest")

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, map[string]*deps.LockedDep{
		"github.com/x/core//lib": {Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "abc123"},
	}, lock.Deps)
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
	require.NoError(t, os.WriteFile(deps.SourceManifestFileName,
		[]byte("manifest: { requires: { 'github.com/x/core': 'v1.0.0' } }\n"), 0o644))

	out, err := runCommandWithRemotes(t, goCoreRemotes(), "deps", "sync")
	require.NoError(t, err)
	require.Contains(t, out, "Wrote manifest.ub")

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, map[string]*deps.LockedDep{
		"github.com/x/core//lib": {Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "abc123"},
	}, lock.Deps)
}

func TestDepsSyncSourceManifest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(`
factory: {
  imports: {
    core: 'github.com/x/core//lib'
  }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.SourceManifestFileName),
		[]byte("manifest: { requires: { 'github.com/x/core': 'v1.0.0' } }\n"), 0o644))

	out, err := runCommandWithRemotes(t, goCoreRemotes(), "deps", "sync",
		"-p", filepath.Join(root, "factory.ub"))
	require.NoError(t, err)
	require.Contains(t, out, "Wrote manifest.ub")

	manifestBytes, err := os.ReadFile(filepath.Join(root, deps.SourceManifestFileName))
	require.NoError(t, err)
	require.Equal(t, `manifest: {
  requires: {
    'github.com/x/core': 'v1.0.0'
  }
}
`, string(manifestBytes))
	_, err = os.Stat(filepath.Join(root, deps.ManifestFileName))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestDepsSyncLibraryProject(t *testing.T) {
	// A library project: body files, no main.ub. Its dependencies are
	// managed the same way a factory's are.
	root := filepath.Join(t.TempDir(), "greeter")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "resource-greeting.ub"),
		[]byte("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		[]byte("requires: {\n  'github.com/x/core': 'v1.0.0'\n}\n"), 0o644))

	_, err := runCommandWithRemotes(t, goCoreRemotes(), "deps", "sync", "-p", root)
	require.NoError(t, err)

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, map[string]*deps.LockedDep{
		"github.com/x/core//lib": {Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "abc123"},
	}, lock.Deps)
}

func TestDepsSyncWithReplace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "factory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.ub"),
		[]byte("imports: { aws: 'github.com/cloudboss/unobin-library-aws' }\n"), 0o644))

	// A local Go library the manifest replaces in (no remote, no floor).
	awsDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "go.mod"),
		[]byte("module github.com/cloudboss/unobin-library-aws\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		[]byte("requires: {}\nreplace: { 'github.com/cloudboss/unobin-library-aws': '"+
			awsDir+"' }\n"), 0o644))

	_, err := runCommand(t, "deps", "sync", "-p", filepath.Join(root, "main.ub"))
	require.NoError(t, err)

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Empty(t, lock.Deps, "a replaced dependency is not locked")
}

func TestDepsSyncRejectsMissingFloor(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.ub"),
		[]byte("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))

	// No manifest, so the imported repo has no floor.
	_, err := runCommand(t, "deps", "sync", "-p", filepath.Join(root, "main.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "github.com/x/core")
	require.Contains(t, err.Error(), "deps get")
}

func TestDepsSyncPrunesStaleFloor(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myfactory")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.ub"),
		[]byte("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))
	// gone/repo is listed but no longer imported; sync must remove it.
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		[]byte("requires: {\n  'github.com/gone/repo': 'v1.0.0'\n"+
			"  'github.com/x/core': 'v1.0.0'\n}\n"), 0o644))

	_, err := runCommandWithRemotes(t, goCoreRemotes(), "deps", "sync",
		"-p", filepath.Join(root, "main.ub"))
	require.NoError(t, err)

	manifestBytes, err := os.ReadFile(filepath.Join(root, deps.ManifestFileName))
	require.NoError(t, err)
	require.Equal(t,
		"requires: {\n  'github.com/x/core': 'v1.0.0'\n}\n", string(manifestBytes))
}

func writeProjectLock(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(root, 0o755))
	lock := deps.NewLock()
	lock.Deps["github.com/x/core//lib"] = &deps.LockedDep{
		Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "c1",
	}
	lock.Deps["github.com/x/hello//ub"] = &deps.LockedDep{
		Kind: deps.LockKindUB, Version: "v2.0.0", Commit: "c2", Hash: "h2",
	}
	require.NoError(t, deps.WriteLock(filepath.Join(root, deps.LockFileName), lock))
}

func TestDepsList(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	writeProjectLock(t, root)
	out, err := runCommand(t, "deps", "list", "-p", filepath.Join(root, "main.ub"))
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
	_, err := runCommand(t, "deps", "list", "-p", filepath.Join(root, "main.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "run `unobin deps sync`")
}

func TestDepsVerifyMatches(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	writeProjectLock(t, root)
	remotes := map[string]*resolve.Source{
		"github.com/x/hello//ub@c2": {Hash: "h2"},
	}
	out, err := runCommandWithRemotes(t, remotes, "deps", "verify",
		"-p", filepath.Join(root, "main.ub"))
	require.NoError(t, err)
	require.Contains(t, out, "all dependencies verified")
}

func TestDepsVerifyDetectsMismatch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	writeProjectLock(t, root)
	remotes := map[string]*resolve.Source{
		"github.com/x/hello//ub@c2": {Hash: "tampered"},
	}
	_, err := runCommandWithRemotes(t, remotes, "deps", "verify",
		"-p", filepath.Join(root, "main.ub"))
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
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.ub"),
		[]byte("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))
	stubListTags(t, map[string][]string{
		"github.com/x/core": {"v1.0.0", "v1.2.0", "v2.0.0"},
	})
	return root
}

func goLibRemotes(version, commit string) map[string]*resolve.Source {
	return map[string]*resolve.Source{
		"github.com/x/core@" + version: {FS: fstest.MapFS{}},
		"github.com/x/core//lib@" + version: {
			Commit: commit,
			FS:     fstest.MapFS{"lib.go": &fstest.MapFile{Data: []byte("package lib")}},
		},
	}
}

// writeCompileLock writes a unobin.lock into dir pinning each id (a
// repo//subdir or bare Go path) to a version. Compile takes versions from
// the lock, so a fixture with versionless imports needs one. Each entry is
// recorded as a Go library, which is all compile reads from the lock.
func writeCompileLock(t *testing.T, dir string, pins map[string]string) {
	t.Helper()
	lock := deps.NewLock()
	for id, version := range pins {
		lock.Deps[id] = &deps.LockedDep{Kind: deps.LockKindGo, Version: version, Commit: "c"}
	}
	require.NoError(t, deps.WriteLock(filepath.Join(dir, deps.LockFileName), lock))
}

func TestDepsGetExactVersion(t *testing.T) {
	root := writeGetProject(t)
	out, err := runCommandWithRemotes(t, goLibRemotes("v1.2.0", "c12"),
		"deps", "get", "github.com/x/core@v1.2.0", "-p", filepath.Join(root, "main.ub"))
	require.NoError(t, err)
	require.Contains(t, out, "github.com/x/core v1.2.0")

	manifestBytes, err := os.ReadFile(filepath.Join(root, deps.ManifestFileName))
	require.NoError(t, err)
	require.Equal(t, "requires: {\n  'github.com/x/core': 'v1.2.0'\n}\n", string(manifestBytes))

	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, "v1.2.0", lock.Deps["github.com/x/core//lib"].Version)
}

func TestDepsGetLatest(t *testing.T) {
	root := writeGetProject(t)
	_, err := runCommandWithRemotes(t, goLibRemotes("v2.0.0", "c20"),
		"deps", "get", "github.com/x/core", "-p", filepath.Join(root, "main.ub"))
	require.NoError(t, err)
	lock, err := deps.ReadLock(os.DirFS(root))
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", lock.Deps["github.com/x/core//lib"].Version)
}

func TestCompileToStdout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	src := `
imports: { core: 'github.com/cloudboss/unobin/pkg/libraries/core' }
actions: { core.command.hi: { argv: ['echo', 'hi'] } }
`
	require.NoError(t, os.WriteFile(stackPath, []byte(src), 0o644))
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
	require.NoError(t, os.WriteFile(stackPath, []byte(src), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin/pkg/libraries/core": "v0.1.0",
	})

	out, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.NoError(t, err)

	require.Contains(t, out, "package main")
	require.Contains(t, out, `core.command.hi:`)
	require.NotContains(t, out, "factory: {")
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
	require.Contains(t, out, `core.command.hi:`)
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
	require.Contains(t, out, `core.command.hi:`)
}

// TestCompileWriteOut compiles to an output directory and checks the
// files written there. The import deliberately lives under the unobin
// module path: such a library is served by the unobin require the
// generated go.mod always pins, with no require line of its own.
func TestCompileWriteOut(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	src := `
imports: {
  core: 'github.com/cloudboss/unobin/pkg/libraries/core'
}
`
	require.NoError(t, os.WriteFile(stackPath, []byte(src), 0o644))
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

// TestCompileUsesLockVersion compiles a factory whose import carries no
// @version: the version must come from unobin.lock. The fake resolver only
// serves the locked version, and the generated go.mod must record it.
func TestCompileUsesLockVersion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"),
		[]byte("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))
	lock := deps.NewLock()
	lock.Deps["github.com/x/core//lib"] = &deps.LockedDep{
		Kind: deps.LockKindGo, Version: "v1.0.0", Commit: "c1",
	}
	require.NoError(t, deps.WriteLock(filepath.Join(dir, deps.LockFileName), lock))

	remotes := map[string]*resolve.Source{
		"github.com/x/core//lib@v1.0.0": {
			FS: fstest.MapFS{"lib.go": &fstest.MapFile{Data: []byte("package lib")}},
		},
	}
	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommandWithRemotes(t, remotes, "compile",
		"-p", filepath.Join(dir, "main.ub"), "-o", outDir)
	require.NoError(t, err)
	goMod, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(goMod), "github.com/x/core/lib v1.0.0")
}

// TestCompileRequiresLockedVersion compiles a factory whose import is
// versionless and has no unobin.lock. Compile never selects a version on
// its own, so it must fail and point at deps sync.
// TestCompileWithReplacedGoLibrary compiles a factory that imports a Go
// library by URL while the manifest replaces it with a local checkout. The
// import needs no locked version, and the generated go.mod requires the
// module and replaces it with the local path.
func TestCompileWithReplacedGoLibrary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"),
		[]byte("imports: { aws: 'github.com/cloudboss/unobin-library-aws' }\n"), 0o644))

	awsDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "go.mod"),
		[]byte("module github.com/cloudboss/unobin-library-aws\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "library.go"), []byte(`package aws

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{Name: "aws"}
}
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ManifestFileName),
		[]byte("requires: {}\nreplace: { 'github.com/cloudboss/unobin-library-aws': '"+
			awsDir+"' }\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "main.ub"),
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"),
		[]byte("imports: { core: 'github.com/x/core//lib' }\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "main.ub"), "-o", outDir)
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"),
		[]byte("description: 'minimal'\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "main.ub"), "-o", outDir)
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"),
		[]byte("description: 'minimal'\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "main.ub"), "-o", outDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--replace-unobin")
	require.Contains(t, err.Error(), "unobin.manifest")
}

// TestCompileDevVersionAcceptsManifestReplace proves the manifest's
// replace of the unobin repo satisfies the development gate the same
// way --replace-unobin does, and appears in the generated go.mod.
func TestCompileDevVersionAcceptsManifestReplace(t *testing.T) {
	setCLIVersion(t, "dev")
	rootDir := findUnobinRoot(t)
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"),
		[]byte("description: 'minimal'\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ManifestFileName),
		[]byte("requires: {}\nreplace: { 'github.com/cloudboss/unobin': '"+rootDir+"' }\n"),
		0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "main.ub"), "-o", outDir)
	require.NoError(t, err)

	goMod, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(goMod), "github.com/cloudboss/unobin v0.0.0")
	require.Contains(t, string(goMod), "github.com/cloudboss/unobin => "+rootDir)
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
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"),
			[]byte("description: 'minimal'\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ManifestFileName),
			[]byte(manifest), 0o644))
		return dir
	}

	t.Run("matching line proceeds", func(t *testing.T) {
		dir := write(t, "unobin-version: 'v0.1.0'\nrequires: {}\n")
		outDir := filepath.Join(t.TempDir(), "build")
		_, err := runCommand(t, "compile",
			"-p", filepath.Join(dir, "main.ub"), "-o", outDir)
		require.NoError(t, err)
	})

	t.Run("mismatched line is refused", func(t *testing.T) {
		dir := write(t, "unobin-version: 'v0.9.9'\nrequires: {}\n")
		outDir := filepath.Join(t.TempDir(), "build")
		_, err := runCommand(t, "compile",
			"-p", filepath.Join(dir, "main.ub"), "-o", outDir)
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
			"-p", filepath.Join(dir, "main.ub"), "-o", outDir)
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

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"), []byte(`
imports: {
  net: './libraries/net'
  aws: 'github.com/cloudboss/unobin-library-aws'
}
`), 0o644))

	netDir := filepath.Join(dir, "libraries", "net")
	require.NoError(t, os.MkdirAll(netDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(netDir, "resource-cluster.ub"), []byte(`
description: 'a cluster'
resources:   { local.file.x: { path: '/tmp/x', content: 'hi', mode: 420 } }
`), 0o644))

	awsDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "go.mod"),
		[]byte("module github.com/cloudboss/unobin-library-aws\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "library.go"), []byte(`package aws

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{Name: "aws"}
}
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ManifestFileName),
		[]byte("requires: {}\nreplace: {\n"+
			"  'github.com/cloudboss/unobin': '"+rootDir+"'\n"+
			"  'github.com/cloudboss/unobin-library-aws': '"+awsDir+"'\n"+
			"}\n"), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "main.ub"), "-o", outDir)
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"),
		[]byte("imports: { x: 'github.com/cloudboss/unobin//examples/thing' }\n"), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin": "v0.5.0",
	})

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "main.ub"), "-o", outDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolchain-versioned")
	require.Contains(t, err.Error(), "replace")
}

// TestDepsGetRefusesUnobin proves a floor cannot be added for the
// unobin repository.
func TestDepsGetRefusesUnobin(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"),
		[]byte("description: 'x'\n"), 0o644))

	_, err := runCommand(t, "deps", "get", "github.com/cloudboss/unobin@v0.5.0",
		"-p", filepath.Join(dir, "main.ub"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolchain-versioned")
}

// TestDepsSyncTeachesReplaceForUnobinImport proves sync does not ask
// for a floor on an unobin-repo import; the fix it teaches is the
// replace.
func TestDepsSyncTeachesReplaceForUnobinImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"),
		[]byte("imports: { x: 'github.com/cloudboss/unobin//examples/thing' }\n"), 0o644))

	_, err := runCommand(t, "deps", "sync", "-p", filepath.Join(dir, "main.ub"))
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
	factoryPath := filepath.Join(srcDir, "main.ub")
	require.NoError(t, os.WriteFile(factoryPath,
		[]byte("description: 'minimal'\n"), 0o644))

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
	factoryPath := filepath.Join(srcDir, "main.ub")
	require.NoError(t, os.WriteFile(factoryPath,
		[]byte("description: 'minimal'\n"), 0o644))

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
resources: { local.file.bad: { path: var.missing } }
`), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown input "missing"`)
}

func TestCompileUnimportedResourceModuleFails(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports:   { std: 'github.com/cloudboss/unobin-library-std' }
resources: { greeter.greeting.welcome: { message: 'hello' } }
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
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports:   { fake: 'example.com/fake' }
resources: { fake.thing.x: {} }
outputs:   { bad: { value: resource.fake.thing.x.nonexistent } }
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
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports:   { fake: 'example.com/fake' }
resources: { fake.thing.x: {} }
outputs:   { good: { value: resource.fake.thing.x.id } }
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
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports:   { partial: 'example.com/partial' }
resources: { partial.thing.x: {} }
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
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"),
		[]byte("imports: { lib: './lib' }\n"), 0o644))
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "lib", name), []byte(body), 0o644))
	}
	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", filepath.Join(dir, "main.ub"),
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
			file: "data-lookup.ub",
			body: "outputs: { v: { value: 'hi' } }\n",
		},
		{
			name: "valid action composite",
			file: "action-deploy.ub",
			body: "actions: { core.command.c: { argv: ['echo'] } }\n",
		},
		{
			name: "valid resource composite",
			file: "resource-box.ub",
			body: "resources: { local.file.x: { path: '/tmp/x' } }\n",
		},
		{
			name: "data without output",
			file: "data-lookup.ub",
			body: "data: { aws.ami.x: { most-recent: true } }\n",
			wantErr: `import "lib": composite "lookup" (data): ` +
				`a data composite must declare at least one output`,
		},
		{
			name: "data with a resource",
			file: "data-lookup.ub",
			body: "resources: { local.file.x: {} }\n" +
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
			body: "data: { aws.ami.x: {} }\n",
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
		"data-a.ub":     "data: { aws.ami.x: {} }\n",
		"resource-b.ub": "data: { aws.ami.x: {} }\n",
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
resources:   { local.file.x: { path: '/tmp/x', content: 'hi', mode: 420 } }
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
	factoryBody        = "\nimports: {\n  net: './libraries/net'\n}\n"
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
				Name: "cluster",
				Kind: runtime.NodeResource,
				Body: &lang.File{Kind: lang.FileExportedType, Path: "resource-cluster.ub", Body: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "description"}, Value: &lang.StringLit{Value: "a cluster"}}, {Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "resources"}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldPath, Path: []string{"local", "file", "x"}}, Value: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "path"}, Value: &lang.StringLit{Value: "/tmp/x"}}, {Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "content"}, Value: &lang.StringLit{Value: "hi"}}, {Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "mode"}, Value: &lang.NumberLit{Value: "420", ParsedInt: 420}}}}}}}}}}},
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
resources:   { local.file.x: { path: '/tmp/x', content: 'hi', mode: 420 } }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
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

func TestCompileNestedUBLibraries(t *testing.T) {
	// inner library: a remote UB library the outer one imports.
	innerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "resource-hello.ub"), []byte(`
description: 'inner hello'
inputs:      { path: { type: string } }
imports:     { std: 'github.com/cloudboss/unobin-library-std' }
resources:   { std.file.this: { path: var.path, content: 'hi' } }
outputs:     { path: { value: resource.std.file.this.path } }
`), 0o644))

	// outer library: imports inner under a different alias and wraps it.
	outerDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outerDir, "resource-greeting.ub"), []byte(`
description: 'outer greeting'
inputs:      { path: { type: string } }
imports:     { inner: 'github.com/example/inner//ub/inner' }
resources:   { inner.hello.x: { path: var.path } }
outputs:     { path: { value: resource.inner.hello.x.path } }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
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
	require.NoError(t, os.WriteFile(filepath.Join(aDir, "resource-type-a.ub"), []byte(`
description: 'a body'
imports:     { b: 'github.com/example/b//ub/b' }
resources:   { b.type-b.y: {} }
`), 0o644))

	bDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bDir, "resource-type-b.ub"), []byte(`
description: 'b body'
imports:     { a: 'github.com/example/a//ub/a' }
resources:   { a.type-a.z: {} }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
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
	require.NoError(t, os.WriteFile(filepath.Join(innerDir, "resource-hello.ub"), []byte(`
description: 'inner hello'
inputs:      { path: { type: string } }
resources:   { local.file.x: { path: var.path, content: 'hi' } }
outputs:     { path: { value: resource.local.file.x.path } }
`), 0o644))

	wrapDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(wrapDir, "resource-greeting.ub"), []byte(`
description: 'wrap greeting'
inputs:      { path: { type: string } }
imports:     { inside: 'github.com/example/shared//ub/shared' }
resources:   { inside.hello.x: { path: var.path } }
outputs:     { path: { value: resource.inside.hello.x.path } }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	// Stack root uses alias "shared"; the wrapper composite uses
	// alias "inside" for the same URL.
	require.NoError(t, os.WriteFile(stackPath, []byte(`
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
		filepath.Join(fakeUnobin, "some-lib", "resource-foo.ub"), []byte(`
description: 'a foo'
resources:   { local.file.x: { path: '/tmp/x', content: 'hi', mode: 420 } }
`), 0o644))

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
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
  local: 'github.com/cloudboss/unobin//pkg/libraries/local'
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin//pkg/libraries/local": "v0.1.0",
	})

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
	stackPath := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
imports: {
  std: 'github.com/cloudboss/unobin-library-std//fs'
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin-library-std": "v0.1.0",
	})

	out, err := runCommand(t, "compile", "-p", stackPath, "-o", "-",
		"--version", "v0.1.0")
	require.NoError(t, err)
	require.Contains(t, out, `"github.com/cloudboss/unobin-library-std/fs"`)
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
