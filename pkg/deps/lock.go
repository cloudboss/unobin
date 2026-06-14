package deps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
)

const (
	// LockFileName is the legacy JSON dependency lock filename.
	LockFileName = "unobin.lock"

	// SourceLockFileName is the grammar-first dependency lock filename.
	SourceLockFileName = "lock.ub"
)

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

// Lock is the on-disk schema for the dependency lock. It pins the full
// resolved set so compiles are reproducible without re-running selection.
// Deps is keyed by dependency id (a repo URL with an optional `//subdir`),
// the same form a manifest `requires:` key uses.
type Lock struct {
	Version          int                   `json:"version"`
	ToolchainVersion string                `json:"-"`
	Deps             map[string]*LockedDep `json:"deps"`
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

// ReadLock reads and parses lock.ub or the legacy unobin.lock from fsys. A
// missing file returns an error wrapping fs.ErrNotExist, which callers can
// detect with errors.Is.
func ReadLock(fsys fs.FS) (*Lock, error) {
	source, sourceErr := fs.ReadFile(fsys, SourceLockFileName)
	legacy, legacyErr := fs.ReadFile(fsys, LockFileName)
	if sourceErr == nil && legacyErr == nil {
		return nil, fmt.Errorf("lock: found both %s and %s; keep one lock file",
			SourceLockFileName, LockFileName)
	}
	if sourceErr == nil {
		return DecodeSourceLock(source)
	}
	if !errors.Is(sourceErr, fs.ErrNotExist) {
		return nil, sourceErr
	}
	if legacyErr != nil {
		return nil, legacyErr
	}
	return DecodeLock(legacy)
}

// DecodeLock parses a legacy JSON lock from bytes.
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

// DecodeSourceLock parses a grammar-first lock.ub from bytes.
func DecodeSourceLock(b []byte) (*Lock, error) {
	f, err := syntax.ParseSource(SourceLockFileName, b)
	if err != nil {
		return nil, err
	}
	if errs := syntax.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return parseSourceLockBody(f)
}

func parseSourceLockBody(f *syntax.File) (*Lock, error) {
	if f == nil || f.Lock == nil {
		return nil, fmt.Errorf("lock: %s must declare lock", SourceLockFileName)
	}
	lock := &Lock{
		Version: int(f.Lock.Version.ParsedInt),
		Deps:    make(map[string]*LockedDep, len(f.Lock.Deps)),
	}
	if f.Lock.Toolchain != nil && f.Lock.Toolchain.UnobinVersion != nil {
		lock.ToolchainVersion = f.Lock.Toolchain.UnobinVersion.Value
	}
	for _, dep := range f.Lock.Deps {
		locked := &LockedDep{
			Kind:    LockKind(dep.Kind.Name),
			Version: dep.Version.Value,
			Commit:  dep.Commit.Value,
		}
		if dep.Hash != nil {
			locked.Hash = dep.Hash.Value
		}
		lock.Deps[dep.ID.Value] = locked
	}
	if err := validateSourceLock(lock); err != nil {
		return nil, err
	}
	return lock, nil
}

// WriteLock serializes a legacy JSON lock and atomically replaces the file
// at path. The output is pretty-printed JSON with sorted keys for stable
// diffs.
func WriteLock(path string, lock *Lock) error {
	b, err := EncodeLock(lock)
	if err != nil {
		return err
	}
	return writeLockBytes(path, b)
}

// WriteSourceLock serializes lock as canonical lock.ub source and
// atomically replaces the file at path.
func WriteSourceLock(path string, lock *Lock) error {
	b, err := EncodeSourceLock(lock)
	if err != nil {
		return err
	}
	return writeLockBytes(path, b)
}

func writeLockBytes(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EncodeLock serializes a legacy JSON lock to bytes with a trailing
// newline.
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

// EncodeSourceLock serializes lock as canonical lock.ub source.
func EncodeSourceLock(lock *Lock) ([]byte, error) {
	if err := validateSourceLock(lock); err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "lock: { version: %d toolchain: { unobin-version: %s } deps: { ",
		lock.Version, sourceString(lock.ToolchainVersion))
	for _, id := range lock.SortedIDs() {
		dep := lock.Deps[id]
		fmt.Fprintf(&b, "%s: { kind: %s version: %s commit: %s",
			sourceString(id), dep.Kind, sourceString(dep.Version), sourceString(dep.Commit))
		if dep.Hash != "" {
			fmt.Fprintf(&b, " hash: %s", sourceString(dep.Hash))
		}
		b.WriteString(" } ")
	}
	b.WriteString("} }")
	out, err := lang.Canonicalize(SourceLockFileName, []byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	return out, nil
}

func sourceString(s string) string {
	repl := strings.NewReplacer(
		"\\", "\\\\",
		"'", "\\'",
		"\n", `\n`,
		"\t", `\t`,
		"\r", `\r`,
		"\x00", `\0`,
	)
	return "'" + repl.Replace(s) + "'"
}

func validateSourceLock(l *Lock) error {
	if l == nil {
		return fmt.Errorf("lock: nil lock")
	}
	if l.Version != CurrentLockVersion {
		return fmt.Errorf("lock: unsupported version %d (this build expects %d)",
			l.Version, CurrentLockVersion)
	}
	if l.ToolchainVersion == "" {
		return fmt.Errorf("lock: missing toolchain unobin-version")
	}
	if err := validateLockedDeps(l); err != nil {
		return err
	}
	for id, dep := range l.Deps {
		if dep.Kind == LockKindUB && !hasHashAlgorithm(dep.Hash) {
			return fmt.Errorf("lock: dependency %q: hash must include an algorithm prefix", id)
		}
	}
	return nil
}

func hasHashAlgorithm(hash string) bool {
	for i, r := range hash {
		if r == ':' {
			return i > 0 && i < len(hash)-1
		}
	}
	return false
}

func validateLockedDeps(l *Lock) error {
	if l == nil {
		return fmt.Errorf("lock: nil lock")
	}
	if l.Deps == nil {
		return fmt.Errorf("lock: missing `deps`")
	}
	for id, dep := range l.Deps {
		if _, err := ParseDependency(id); err != nil {
			return fmt.Errorf("lock: dependency %q: %w", id, err)
		}
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
