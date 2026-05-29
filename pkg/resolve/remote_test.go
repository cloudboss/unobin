package resolve

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// makeRemoteRepo initializes a git repo at dir with the file tree
// supplied by files (path -> body) and a tag `v1` pointing at HEAD.
// Returns the commit SHA.
func makeRemoteRepo(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	run := func(args ...string) string {
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

func TestRemoteResolverFetchesUBLibrary(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	wantSHA := makeRemoteRepo(t, src, map[string]string{
		"resource-cluster.ub": "description: 'remote'\n",
	})

	r := &RemoteResolver{CacheRoot: t.TempDir()}
	got, err := r.Resolve(&RemoteImport{URL: src, Version: "v1"})
	require.NoError(t, err)
	require.Equal(t, wantSHA, got.Commit)
	require.NotEmpty(t, got.Hash)
	require.NotNil(t, got.FS)

	body, err := fs.ReadFile(got.FS, "resource-cluster.ub")
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
	require.Empty(t, got.Hash, "go-library imports do not record a content hash")
	require.NotNil(t, got.FS, "every resolved source exposes a filesystem")
}

func TestRemoteResolverHonorsSubdir(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	makeRemoteRepo(t, src, map[string]string{
		"libraries/net/resource-cluster.ub": "description: 'net'\n",
		"go.mod":                            "module example.com/x\n",
	})

	r := &RemoteResolver{CacheRoot: t.TempDir()}
	got, err := r.Resolve(&RemoteImport{URL: src, Subdir: "libraries/net", Version: "v1"})
	require.NoError(t, err)
	require.NotNil(t, got.FS)
	require.NotEmpty(t, got.Hash)

	body, err := fs.ReadFile(got.FS, "resource-cluster.ub")
	require.NoError(t, err)
	require.Contains(t, string(body), "net")
}

func TestRemoteResolverCacheHitSkipsRefetch(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	makeRemoteRepo(t, src, map[string]string{
		"library.ub": "description: 'first'\n",
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
