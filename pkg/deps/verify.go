package deps

import (
	"fmt"
	"io/fs"

	"github.com/cloudboss/unobin/pkg/projectmarker"
	"github.com/cloudboss/unobin/pkg/resolve"
)

// Verify re-fetches every ub-kind dependency in lock at its pinned commit
// and checks the content hash against the recorded one. A mismatch means
// the source changed under a pinned commit. Go dependencies are skipped:
// their integrity rides the generated go.sum, not a content hash here. It
// returns one message per dependency whose hash no longer matches, in id
// order.
func Verify(lock *Lock, resolver resolve.Resolver) ([]string, error) {
	var mismatches []string
	for _, id := range lock.SortedIDs() {
		entry := lock.Deps[id]
		if entry.Kind != LockKindUB {
			continue
		}
		url, subdir, err := resolve.SplitRepoSubdir(id)
		if err != nil {
			return nil, fmt.Errorf("lock id %q: %w", id, err)
		}
		src, err := resolver.Resolve(
			&resolve.RemoteImport{URL: url, Subdir: subdir, Version: entry.Commit})
		if err != nil {
			return nil, fmt.Errorf("verify %s: %w", id, err)
		}
		if err := requireUBProjectMarker(src.FS); err != nil {
			return nil, fmt.Errorf("verify %s: %w", id, err)
		}
		hash, err := HashUBProject(src.FS)
		if err != nil {
			return nil, fmt.Errorf("verify %s: %w", id, err)
		}
		if hash != entry.Hash {
			mismatches = append(mismatches,
				fmt.Sprintf("%s: hash mismatch (locked %s, got %s)", id, entry.Hash, hash))
		}
	}
	return mismatches, nil
}

func requireUBProjectMarker(fsys fs.FS) error {
	if fsys == nil {
		return fmt.Errorf("expected UB project marker")
	}
	marker, err := projectmarker.ClassifyRoot(fsys)
	if err != nil {
		return err
	}
	if marker.Kind != projectmarker.UB {
		return fmt.Errorf("expected UB project marker")
	}
	return nil
}
