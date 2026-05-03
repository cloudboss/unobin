// Package git wraps the go-git operations the resolver needs:
// resolving a ref to a commit SHA without fetching, and cloning a
// repo at a specific ref into a directory.
package git

import (
	"context"
	"fmt"
	"os"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
)

// LsRemote returns the commit SHA that ref points at on the remote at
// url. ref may be a tag, branch, or a full commit SHA. No local clone
// is created.
func LsRemote(ctx context.Context, url, ref string) (string, error) {
	rem := gogit.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})
	refs, err := rem.ListContext(ctx, &gogit.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("ls-remote %s: %w", url, err)
	}
	wantTag := plumbing.NewTagReferenceName(ref).String()
	wantBranch := plumbing.NewBranchReferenceName(ref).String()
	for _, r := range refs {
		name := r.Name().String()
		if name == wantTag || name == wantBranch {
			return r.Hash().String(), nil
		}
	}
	if plumbing.IsHash(ref) {
		return ref, nil
	}
	return "", fmt.Errorf("ls-remote %s: no ref matches %q", url, ref)
}

// Clone clones the repo at url into dest and checks out ref. dest
// must not yet exist. Returns the resolved commit SHA.
func Clone(ctx context.Context, url, ref, dest string) (string, error) {
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("clone %s: %s already exists", url, dest)
	}
	repo, err := gogit.PlainCloneContext(ctx, dest, false, &gogit.CloneOptions{
		URL: url,
	})
	if err != nil {
		return "", fmt.Errorf("clone %s: %w", url, err)
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", ref, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("worktree: %w", err)
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{Hash: *hash}); err != nil {
		return "", fmt.Errorf("checkout %s: %w", ref, err)
	}
	return hash.String(), nil
}
