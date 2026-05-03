package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// makeRepo initializes a git repo at dir with a single commit on
// `main` and a tag `v1` pointing at it. Returns the commit SHA.
func makeRepo(t *testing.T, dir string) string {
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi\n"), 0o644))
	run("add", "hello.txt")
	run("commit", "--quiet", "-m", "first")
	run("tag", "v1")
	return strings.TrimSpace(run("rev-parse", "HEAD"))
}

func TestLsRemoteResolvesTag(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	wantSHA := makeRepo(t, src)

	got, err := LsRemote(context.Background(), src, "v1")
	require.NoError(t, err)
	require.Equal(t, wantSHA, got)
}

func TestLsRemoteResolvesBranch(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	wantSHA := makeRepo(t, src)

	got, err := LsRemote(context.Background(), src, "main")
	require.NoError(t, err)
	require.Equal(t, wantSHA, got)
}

func TestLsRemotePassesThroughHash(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	wantSHA := makeRepo(t, src)

	got, err := LsRemote(context.Background(), src, wantSHA)
	require.NoError(t, err)
	require.Equal(t, wantSHA, got)
}

func TestLsRemoteUnknownRef(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	makeRepo(t, src)

	_, err := LsRemote(context.Background(), src, "no-such-ref")
	require.Error(t, err)
}

func TestCloneCheckoutByTag(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	wantSHA := makeRepo(t, src)

	dest := filepath.Join(t.TempDir(), "dest")
	gotSHA, err := Clone(context.Background(), src, "v1", dest)
	require.NoError(t, err)
	require.Equal(t, wantSHA, gotSHA)

	body, err := os.ReadFile(filepath.Join(dest, "hello.txt"))
	require.NoError(t, err)
	require.Equal(t, "hi\n", string(body))
}

func TestCloneCheckoutByCommit(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	wantSHA := makeRepo(t, src)

	dest := filepath.Join(t.TempDir(), "dest")
	gotSHA, err := Clone(context.Background(), src, wantSHA, dest)
	require.NoError(t, err)
	require.Equal(t, wantSHA, gotSHA)
}

func TestCloneRejectsExistingDest(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	makeRepo(t, src)

	dest := filepath.Join(t.TempDir(), "dest")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "x"), []byte{}, 0o644))

	_, err := Clone(context.Background(), src, "v1", dest)
	require.Error(t, err)
}

func TestCloneUnknownRef(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	makeRepo(t, src)
	dest := filepath.Join(t.TempDir(), "dest")

	_, err := Clone(context.Background(), src, "no-such-ref", dest)
	require.Error(t, err)
}
