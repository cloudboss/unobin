package resolve

import (
	"encoding/json"
	"fmt"
	"os"
)

// LockFileName is the standard filename for a stack or module's lock file.
const LockFileName = "unobin.lock"

// CurrentLockVersion is the schema version this package reads and writes.
// Older versions error on read; newer versions error too, since this build
// can't guarantee correct interpretation.
const CurrentLockVersion = 1

// LockKind is the category of an imported module recorded in the lock.
type LockKind string

const (
	LockKindGoModule LockKind = "go-module"
	LockKindUBModule LockKind = "ub-module"
)

// LockFile is the on-disk schema for `unobin.lock`. Pins each import's
// resolved git commit and a content hash of the relevant subdirectory so
// compiles are reproducible across machines.
type LockFile struct {
	Version int                   `json:"version"`
	Imports map[string]*LockEntry `json:"imports"`
}

// LockEntry records one resolved import.
type LockEntry struct {
	Kind           LockKind `json:"kind"`
	URL            string   `json:"url"`
	Subdir         string   `json:"subdir,omitempty"`
	Constraint     string   `json:"constraint"`
	ResolvedCommit string   `json:"resolved-commit"`
	SubdirHash     string   `json:"subdir-hash"`
}

// NewLockFile returns an empty lock at the current schema version.
func NewLockFile() *LockFile {
	return &LockFile{
		Version: CurrentLockVersion,
		Imports: make(map[string]*LockEntry),
	}
}

// ReadLockFile parses the lock at path. If the file does not exist, the
// underlying error wraps fs.ErrNotExist so callers can detect it with
// errors.Is.
func ReadLockFile(path string) (*LockFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeLockFile(b)
}

// DecodeLockFile parses a lock file from bytes.
func DecodeLockFile(b []byte) (*LockFile, error) {
	var lf LockFile
	if err := json.Unmarshal(b, &lf); err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	if lf.Version != CurrentLockVersion {
		return nil, fmt.Errorf("lock: unsupported version %d (this build expects %d)",
			lf.Version, CurrentLockVersion)
	}
	if err := validateLockEntries(&lf); err != nil {
		return nil, err
	}
	return &lf, nil
}

// WriteLockFile serializes lock and atomically replaces path. The output
// is JSON pretty-printed with two-space indent for readable diffs. Map
// keys are sorted by encoding/json so the bytes are deterministic.
func WriteLockFile(path string, lock *LockFile) error {
	b, err := EncodeLockFile(lock)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EncodeLockFile serializes lock to bytes with a trailing newline.
func EncodeLockFile(lock *LockFile) ([]byte, error) {
	if err := validateLockEntries(lock); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	return append(b, '\n'), nil
}

func validateLockEntries(lf *LockFile) error {
	for alias, entry := range lf.Imports {
		if entry == nil {
			return fmt.Errorf("lock: import %q: nil entry", alias)
		}
		switch entry.Kind {
		case LockKindGoModule, LockKindUBModule:
		case "":
			return fmt.Errorf("lock: import %q: missing `kind`", alias)
		default:
			return fmt.Errorf("lock: import %q: unknown kind %q", alias, entry.Kind)
		}
		if entry.URL == "" {
			return fmt.Errorf("lock: import %q: missing `url`", alias)
		}
		if entry.Constraint == "" {
			return fmt.Errorf("lock: import %q: missing `constraint`", alias)
		}
		if entry.ResolvedCommit == "" {
			return fmt.Errorf("lock: import %q: missing `resolved-commit`", alias)
		}
		if entry.SubdirHash == "" {
			return fmt.Errorf("lock: import %q: missing `subdir-hash`", alias)
		}
	}
	return nil
}
