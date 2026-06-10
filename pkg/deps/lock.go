package deps

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"slices"

	"github.com/cloudboss/unobin/pkg/resolve"
)

// LockFileName is the standard filename for a factory's dependency lock.
const LockFileName = "unobin.lock"

// CurrentLockVersion is the schema version this package reads and writes.
// A different version errors on read, since this build cannot guarantee a
// correct interpretation.
const CurrentLockVersion = 1

// LockKind records how a locked dependency's integrity is guaranteed: a
// `ub` dependency holds a content hash of its source tree, while a `go`
// dependency rides the generated module's go.sum and records only a
// commit.
type LockKind string

const (
	LockKindGo LockKind = "go"
	LockKindUB LockKind = "ub"
)

// Lock is the on-disk schema for unobin.lock. It pins the full resolved
// set so compiles are reproducible without re-running selection. Deps is
// keyed by dependency id (a repo URL with an optional `//subdir`), the
// same form a manifest `requires:` key uses.
type Lock struct {
	Version int                   `json:"version"`
	Deps    map[string]*LockedDep `json:"deps"`
}

// LockedDep is one resolved dependency. Version is the selected git tag;
// the floor lives in the manifest and is never copied here. Hash is the
// source-tree content hash for `ub` dependencies and is omitted for `go`.
type LockedDep struct {
	Kind    LockKind `json:"kind"`
	Version string   `json:"version"`
	Commit  string   `json:"commit"`
	Hash    string   `json:"hash,omitempty"`
}

// NewLock returns an empty lock at the current schema version.
func NewLock() *Lock {
	return &Lock{Version: CurrentLockVersion, Deps: map[string]*LockedDep{}}
}

// SortedIDs returns the lock's dependency ids in sorted order.
func (l *Lock) SortedIDs() []string {
	ids := make([]string, 0, len(l.Deps))
	for id := range l.Deps {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// RepoVersions maps each repository to its selected version, derived from
// the per-library entries (every library of a repo shares its version).
// Compile feeds this to the import walk so versionless imports resolve at
// the locked version.
func (l *Lock) RepoVersions() (map[string]string, error) {
	out := make(map[string]string, len(l.Deps))
	for id, entry := range l.Deps {
		url, _, err := resolve.SplitRepoSubdir(id)
		if err != nil {
			return nil, fmt.Errorf("lock id %q: %w", id, err)
		}
		out[url] = entry.Version
	}
	return out, nil
}

// ReadLock reads and parses unobin.lock from fsys. A missing file returns
// an error wrapping fs.ErrNotExist, which callers can detect with
// errors.Is.
func ReadLock(fsys fs.FS) (*Lock, error) {
	b, err := fs.ReadFile(fsys, LockFileName)
	if err != nil {
		return nil, err
	}
	return DecodeLock(b)
}

// DecodeLock parses a lock from bytes.
func DecodeLock(b []byte) (*Lock, error) {
	var l Lock
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	if l.Version != CurrentLockVersion {
		return nil, fmt.Errorf("lock: unsupported version %d (this build expects %d)",
			l.Version, CurrentLockVersion)
	}
	if err := validateLockedDeps(&l); err != nil {
		return nil, err
	}
	return &l, nil
}

// WriteLock serializes lock and atomically replaces the file at path. The
// output is pretty-printed JSON with sorted keys for stable diffs.
func WriteLock(path string, lock *Lock) error {
	b, err := EncodeLock(lock)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EncodeLock serializes lock to bytes with a trailing newline.
func EncodeLock(lock *Lock) ([]byte, error) {
	if err := validateLockedDeps(lock); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	return append(b, '\n'), nil
}

func validateLockedDeps(l *Lock) error {
	for id, dep := range l.Deps {
		if dep == nil {
			return fmt.Errorf("lock: dependency %q: nil entry", id)
		}
		switch dep.Kind {
		case LockKindGo, LockKindUB:
		case "":
			return fmt.Errorf("lock: dependency %q: missing `kind`", id)
		default:
			return fmt.Errorf("lock: dependency %q: unknown kind %q", id, dep.Kind)
		}
		if dep.Version == "" {
			return fmt.Errorf("lock: dependency %q: missing `version`", id)
		}
		if dep.Commit == "" {
			return fmt.Errorf("lock: dependency %q: missing `commit`", id)
		}
		switch dep.Kind {
		case LockKindUB:
			if dep.Hash == "" {
				return fmt.Errorf("lock: dependency %q: ub dependency missing `hash`", id)
			}
		case LockKindGo:
			if dep.Hash != "" {
				return fmt.Errorf("lock: dependency %q: go dependency must not set `hash`", id)
			}
		}
	}
	return nil
}
