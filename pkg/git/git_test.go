package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

// initRepo creates a repo at dir with one commit on `main`, using go-git
// so the tests need no `git` binary on the host. It returns the open
// repository and the commit hash, leaving tagging to the caller.
func initRepo(t *testing.T, dir string) (*gogit.Repository, plumbing.Hash) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	repo, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)
	require.NoError(t, repo.Storer.SetReference(
		plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))))
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi\n"), 0o644))
	_, err = wt.Add("hello.txt")
	require.NoError(t, err)
	sig := &object.Signature{Name: "t", Email: "t@t"}
	commit, err := wt.Commit("first", &gogit.CommitOptions{Author: sig, Committer: sig})
	require.NoError(t, err)
	return repo, commit
}

// makeRepo initializes a git repo at dir with a single commit on `main`
// and a tag `v1` pointing at it. Returns the commit SHA.
func makeRepo(t *testing.T, dir string) string {
	t.Helper()
	repo, commit := initRepo(t, dir)
	_, err := repo.CreateTag("v1", commit, nil)
	require.NoError(t, err)
	return commit.String()
}

func TestListTags(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src")
	repo, commit := initRepo(t, dir)
	sig := &object.Signature{Name: "t", Email: "t@t"}
	tag := func(name string, annotated bool) {
		t.Helper()
		var opts *gogit.CreateTagOptions
		if annotated {
			opts = &gogit.CreateTagOptions{Tagger: sig, Message: name}
		}
		_, err := repo.CreateTag(name, commit, opts)
		require.NoError(t, err)
	}
	tag("v1", false)
	tag("v2.0.0", false)
	tag("v2.1.0", true) // annotated tags are listed once, like lightweight ones
	tag("networking/v1.5.0", false)
	tag("latest", false) // not semver, returned as-is

	got, err := ListTags(context.Background(), dir)
	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{"v1", "v2.0.0", "v2.1.0", "networking/v1.5.0", "latest"}, got)
}

func TestLsRemoteResolvesTag(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	wantSHA := makeRepo(t, src)

	got, err := LsRemote(context.Background(), src, "v1")
	require.NoError(t, err)
	require.Equal(t, wantSHA, got)
}

func TestLsRemoteResolvesAnnotatedTagToCommit(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	repo, commit := initRepo(t, src)
	sig := &object.Signature{Name: "t", Email: "t@t"}
	_, err := repo.CreateTag("v1", commit, &gogit.CreateTagOptions{
		Tagger:  sig,
		Message: "v1",
	})
	require.NoError(t, err)

	got, err := LsRemote(context.Background(), src, "v1")
	require.NoError(t, err)
	require.Equal(t, commit.String(), got)
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
