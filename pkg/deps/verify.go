package deps

import (
	"fmt"
	"io/fs"

	"github.com/cloudboss/unobin/pkg/projectmarker"
	"github.com/cloudboss/unobin/pkg/resolve"
)

// VerifyMismatch describes one dependency whose pinned content hash changed.
type VerifyMismatch struct {
	ID           string `json:"id"            ub:"id"`
	ExpectedHash string `json:"expected-hash" ub:"expected-hash"`
	ActualHash   string `json:"actual-hash"   ub:"actual-hash"`
	Message      string `json:"message"       ub:"message"`
}

// VerifyResult records the UB dependencies checked and every mismatch.
type VerifyResult struct {
	Checked    int              `json:"checked"    ub:"checked"`
	Mismatches []VerifyMismatch `json:"mismatches" ub:"mismatches"`
}

// Verify checks every UB dependency's pinned content hash.
func Verify(projectLock *ProjectLock, resolver resolve.Resolver) (*VerifyResult, error) {
	result := &VerifyResult{Mismatches: []VerifyMismatch{}}
	for _, id := range projectLock.SortedIDs() {
		entry := projectLock.Deps[id]
		if entry.Kind != ProjectLockKindUB {
			continue
		}
		url, subdir, err := resolve.SplitRepoSubdir(id)
		if err != nil {
			return nil, fmt.Errorf("project-lock id %q: %w", id, err)
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
		result.Checked++
		if hash != entry.Hash {
			result.Mismatches = append(result.Mismatches, VerifyMismatch{
				ID:           id,
				ExpectedHash: entry.Hash,
				ActualHash:   hash,
				Message:      "content hash differs from project-lock.ub",
			})
		}
	}
	return result, nil
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
