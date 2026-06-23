package resolve

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
)

func remoteResolverFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/remote-resolver", name)
}

// makeRemoteRepo initializes a git repo at dir with the file tree
// supplied by files (path -> body) and a tag `v1` pointing at HEAD.
// Returns the commit SHA.
func makeRemoteRepo(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	run := func(args ...string) string {
		t.Helper()
		return runGit(t, dir, args...)
	}
	run("init", "--quiet", "--initial-branch=main")
	for path, body := range files {
		full := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}
	run("add", "-A")
	run("commit", "--quiet", "-m", "first")
	run("tag", "v1")
	return strings.TrimSpace(run("rev-parse", "HEAD"))
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

func TestGitRef(t *testing.T) {
	cases := []struct {
		name string
		ref  *RemoteImport
		want string
	}{
		{name: "root semver", ref: &RemoteImport{URL: "github.com/x/y", Version: "v1.2.3"},
			want: "v1.2.3"},
		{name: "subdir semver", ref: &RemoteImport{
			URL: "github.com/x/y", Subdir: "library-c", Version: "v1.2.3"},
			want: "library-c/v1.2.3"},
		{name: "root project with child package", ref: &RemoteImport{
			URL: "github.com/x/y", Subdir: "ub/helloer", PackageSubdir: "ub/helloer",
			Version: "v1.2.3"}, want: "v1.2.3"},
		{name: "subdir commit", ref: &RemoteImport{
			URL: "github.com/x/y", Subdir: "library-c", Version: "abc123"},
			want: "abc123"},
		{name: "already prefixed", ref: &RemoteImport{
			URL: "github.com/x/y", Subdir: "library-c", Version: "library-c/v1.2.3"},
			want: "library-c/v1.2.3"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, GitRef(tt.ref))
		})
	}
}

func TestRemoteResolverFetchesUBLibrary(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	wantSHA := makeRemoteRepo(t, src, map[string]string{
		"library.ub": "cluster: resource { description: 'remote' }\n",
	})

	r := &RemoteResolver{CacheRoot: t.TempDir()}
	got, err := r.Resolve(&RemoteImport{URL: src, Version: "v1"})
	require.NoError(t, err)
	require.Equal(t, wantSHA, got.Commit)
	require.NotNil(t, got.FS)

	body, err := fs.ReadFile(got.FS, "library.ub")
	require.NoError(t, err)
	require.Contains(t, string(body), "remote")
}

func TestRemoteResolverFetchesGoLibrary(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	wantSHA := makeRemoteRepo(t, src, map[string]string{
		"go.mod":  "module example.com/x\n",
		"main.go": "package x\n",
	})

	r := &RemoteResolver{CacheRoot: t.TempDir()}
	got, err := r.Resolve(&RemoteImport{URL: src, Version: "v1"})
	require.NoError(t, err)
	require.Equal(t, wantSHA, got.Commit)
	require.NotNil(t, got.FS, "every resolved source exposes a filesystem")
}

func TestRemoteResolverHonorsSubdir(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	makeRemoteRepo(t, src, map[string]string{
		"libraries/net/library.ub": "cluster: resource { description: 'net' }\n",
		"go.mod":                   "module example.com/x\n",
	})
	runGit(t, src, "tag", "libraries/net/v1")

	r := &RemoteResolver{CacheRoot: t.TempDir()}
	got, err := r.Resolve(&RemoteImport{URL: src, Subdir: "libraries/net", Version: "v1"})
	require.NoError(t, err)
	require.NotNil(t, got.FS)

	body, err := fs.ReadFile(got.FS, "library.ub")
	require.NoError(t, err)
	require.Contains(t, string(body), "net")
}

func TestRemoteResolverRootProjectServesChildPackage(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	wantSHA := makeRemoteRepo(t, src, map[string]string{
		"project.ub":                   remoteResolverFixture(t, "empty-project"),
		"ub/helloer/resource-hello.ub": remoteResolverFixture(t, "hello-resource"),
	})

	r := &RemoteResolver{CacheRoot: t.TempDir()}
	got, err := r.Resolve(&RemoteImport{
		URL: src, Subdir: "ub/helloer", PackageSubdir: "ub/helloer", Version: "v1",
	})
	require.NoError(t, err)
	require.Equal(t, wantSHA, got.Commit)
	require.NotNil(t, got.FS)
	require.Equal(t, r.cacheDir(src, wantSHA), got.ProjectPath)
	require.Equal(t, filepath.Join(r.cacheDir(src, wantSHA), "ub", "helloer"), got.Path)
	require.Empty(t, got.ProjectSubdir)
	require.Equal(t, "ub/helloer", got.PackageSubdir)

	project, err := fs.ReadFile(got.ProjectFS, "project.ub")
	require.NoError(t, err)
	require.Contains(t, string(project), "project:")

	body, err := fs.ReadFile(got.FS, "resource-hello.ub")
	require.NoError(t, err)
	require.Contains(t, string(body), "hello: resource")
}

func TestRemoteResolverNestedProjectServesChildPackage(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	wantSHA := makeRemoteRepo(t, src, map[string]string{
		"ub/project-b/project.ub":                remoteResolverFixture(t, "empty-project"),
		"ub/project-b/comprehensions/library.ub": remoteResolverFixture(t, "hello-resource"),
	})
	runGit(t, src, "tag", "ub/project-b/v1")

	r := &RemoteResolver{CacheRoot: t.TempDir()}
	got, err := r.Resolve(&RemoteImport{
		URL:           src,
		Subdir:        "ub/project-b/comprehensions",
		ProjectSubdir: "ub/project-b",
		PackageSubdir: "ub/project-b/comprehensions",
		Version:       "v1",
	})
	require.NoError(t, err)
	require.Equal(t, wantSHA, got.Commit)
	require.Equal(t, filepath.Join(r.cacheDir(src, wantSHA), "ub", "project-b"), got.ProjectPath)
	require.Equal(t,
		filepath.Join(r.cacheDir(src, wantSHA), "ub", "project-b", "comprehensions"),
		got.Path)
	require.Equal(t, "ub/project-b", got.ProjectSubdir)
	require.Equal(t, "ub/project-b/comprehensions", got.PackageSubdir)

	project, err := fs.ReadFile(got.ProjectFS, "project.ub")
	require.NoError(t, err)
	require.Contains(t, string(project), "project:")

	body, err := fs.ReadFile(got.FS, "library.ub")
	require.NoError(t, err)
	require.Contains(t, string(body), "hello: resource")
}

func TestRemoteResolverSetsGoModuleMetadata(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	wantSHA := makeRemoteRepo(t, src, map[string]string{
		"go.mod": "module example.com/lib\n",
		"fs/library.go": `package fs

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{Actions: map[string]runtime.ActionType{"x": nil}}
}
`,
	})

	r := &RemoteResolver{CacheRoot: t.TempDir()}
	got, err := r.Resolve(&RemoteImport{
		URL: src, Subdir: "fs", PackageSubdir: "fs", Version: "v1",
	})
	require.NoError(t, err)
	require.Equal(t, wantSHA, got.Commit)
	require.Equal(t, r.cacheDir(src, wantSHA), got.ModuleRootPath)
	require.Equal(t, "example.com/lib", got.ModulePath)
	require.Equal(t, "example.com/lib/fs", got.GoImportPath)
}

func TestRemoteResolverCachedSourceMissingCache(t *testing.T) {
	r := &RemoteResolver{CacheRoot: t.TempDir()}
	got, ok, err := r.CachedSource(&RemoteImport{URL: "github.com/x/y"}, "abc123")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, got)
}

func TestRemoteResolverCachedSourceServesPackageSubdir(t *testing.T) {
	r := &RemoteResolver{CacheRoot: t.TempDir()}
	commit := "abc123"
	cacheDir := r.cacheDir("github.com/x/y", commit)
	require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(cacheDir, "pkg", "library.ub"),
		[]byte("thing: resource { description: 'cached' }\n"),
		0o644,
	))

	got, ok, err := r.CachedSource(&RemoteImport{
		URL: "github.com/x/y", Subdir: "pkg", PackageSubdir: "pkg",
	}, commit)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, commit, got.Commit)
	require.Equal(t, filepath.Join(cacheDir, "pkg"), got.Path)
	require.Equal(t, cacheDir, got.ProjectPath)
	require.Empty(t, got.ProjectSubdir)
	require.Equal(t, "pkg", got.PackageSubdir)

	body, err := fs.ReadFile(got.FS, "library.ub")
	require.NoError(t, err)
	require.Contains(t, string(body), "cached")
}

func TestRemoteResolverCachedSourceServesNestedProject(t *testing.T) {
	r := &RemoteResolver{CacheRoot: t.TempDir()}
	commit := "abc123"
	cacheDir := r.cacheDir("github.com/x/y", commit)
	require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "ub", "project", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(cacheDir, "ub", "project", "project.ub"),
		[]byte(remoteResolverFixture(t, "empty-project")),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(cacheDir, "ub", "project", "pkg", "library.ub"),
		[]byte("thing: resource { description: 'cached' }\n"),
		0o644,
	))

	got, ok, err := r.CachedSource(&RemoteImport{
		URL:           "github.com/x/y",
		Subdir:        "ub/project/pkg",
		ProjectSubdir: "ub/project",
		PackageSubdir: "ub/project/pkg",
	}, commit)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, filepath.Join(cacheDir, "ub", "project"), got.ProjectPath)
	require.Equal(t, filepath.Join(cacheDir, "ub", "project", "pkg"), got.Path)
	require.Equal(t, "ub/project", got.ProjectSubdir)
	require.Equal(t, "ub/project/pkg", got.PackageSubdir)

	project, err := fs.ReadFile(got.ProjectFS, "project.ub")
	require.NoError(t, err)
	require.Contains(t, string(project), "project:")
}

func TestRemoteResolverCachedSourceSetsGoModuleMetadata(t *testing.T) {
	r := &RemoteResolver{CacheRoot: t.TempDir()}
	commit := "abc123"
	cacheDir := r.cacheDir("github.com/x/y", commit)
	require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "fs"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(cacheDir, "go.mod"),
		[]byte("module example.com/lib\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(cacheDir, "fs", "library.go"),
		[]byte("package fs\n"),
		0o644,
	))

	got, ok, err := r.CachedSource(&RemoteImport{
		URL: "github.com/x/y", Subdir: "fs", PackageSubdir: "fs",
	}, commit)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, cacheDir, got.ModuleRootPath)
	require.Equal(t, "example.com/lib", got.ModulePath)
	require.Equal(t, "example.com/lib/fs", got.GoImportPath)
}

func TestRemoteResolverCachedSourceDoesNotConsultRemote(t *testing.T) {
	r := &RemoteResolver{CacheRoot: t.TempDir()}
	commit := "abc123"
	cacheDir := r.cacheDir("/path/that/does/not/exist", commit)
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(cacheDir, "library.ub"),
		[]byte("thing: resource { description: 'cached' }\n"),
		0o644,
	))

	got, ok, err := r.CachedSource(&RemoteImport{URL: "/path/that/does/not/exist"}, commit)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, cacheDir, got.Path)
}

func TestRemoteResolverCacheHitSkipsRefetch(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	makeRemoteRepo(t, src, map[string]string{
		"library.ub": "first: resource { description: 'first' }\n",
	})

	r := &RemoteResolver{CacheRoot: t.TempDir()}
	first, err := r.Resolve(&RemoteImport{URL: src, Version: "v1"})
	require.NoError(t, err)

	cacheDir := r.cacheDir(src, first.Commit)
	lib := filepath.Join(cacheDir, "library.ub")
	require.NoError(t, os.WriteFile(lib, []byte("description: 'overwritten'\n"), 0o644))

	second, err := r.Resolve(&RemoteImport{URL: src, Version: "v1"})
	require.NoError(t, err)
	require.Equal(t, first.Commit, second.Commit)

	body, err := fs.ReadFile(second.FS, "library.ub")
	require.NoError(t, err)
	require.Contains(t, string(body), "overwritten",
		"second Resolve should reuse the cached tree, not refetch")
}

func TestRemoteResolverNormalizesURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"github.com/foo/bar", "github.com/foo/bar"},
		{"https://github.com/foo/bar", "github.com/foo/bar"},
		{"git@github.com:foo/bar", "github.com/foo/bar"},
		{"ssh://git@host/foo/bar", "git@host/foo/bar"},
	}
	for _, c := range cases {
		got := normalizeURL(c.in)
		require.Equal(t, c.want, got, "normalizeURL(%q)", c.in)
	}
}
