// Package git wraps the go-git operations the resolver needs:
// resolving a ref to a commit SHA without fetching, listing a repo's
// tags, and cloning a repo at a specific ref into a directory.
package git

import (
	"context"
	"fmt"
	"os"
	"strings"

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

// ListTags returns the tag names defined on the remote at url, without
// the `refs/tags/` prefix. Peeled annotated-tag refs (the `^{}` entries)
// are excluded so each tag appears once. No local clone is created.
// Order is whatever the remote reports; callers that need ordering sort
// the result themselves.
func ListTags(ctx context.Context, url string) ([]string, error) {
	rem := gogit.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})
	refs, err := rem.ListContext(ctx, &gogit.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("ls-remote %s: %w", url, err)
	}
	var tags []string
	for _, r := range refs {
		name := r.Name()
		if !name.IsTag() {
			continue
		}
		short := name.Short()
		if strings.HasSuffix(short, "^{}") {
			continue
		}
		tags = append(tags, short)
	}
	return tags, nil
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
