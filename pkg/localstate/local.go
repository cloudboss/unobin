package localstate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	ufs "github.com/cloudboss/unobin/pkg/fs"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

const maxRevAttempts = 100

// now returns the current time. Tests override it to freeze the clock
// and force the rev allocator to disambiguate collisions structurally.
var now = time.Now

var _ sdkstate.Backend = (*LocalStore)(nil)

// LocalStore reads and writes snapshots under a per-stack directory.
// Layout is as follows:
//
//	<Root>/<Factory>/<Stack>/
//	  current             // File containing the SHA of the current snapshot.
//	  snapshots/
//	    <rev>.json.enc    // rev is an RFC3339Nano timestamp
//	    ...
type LocalStore struct {
	Root    string
	Factory string

	stack string
	enc   sdkencrypt.Encrypter
	dir   string
}

// Stack returns the stack name this store was constructed
// for. Required by the Backend interface.
func (s *LocalStore) Stack() string { return s.stack }

// NewLocalStore returns a LocalStore for the given factory and stack
// under root, creating the directory tree if it doesn't exist. The
// Encrypter is required, but a pass-through (encrypters.Noop) can be
// passed for tests.
func NewLocalStore(
	root, factory, stack string,
	enc sdkencrypt.Encrypter,
) (*LocalStore, error) {
	if factory == "" {
		return nil, errors.New("local store: factory is required")
	}
	if stack == "" {
		return nil, errors.New("local store: stack is required")
	}
	if enc == nil {
		return nil, errors.New("local store: encrypter is required")
	}
	dir := filepath.Join(root, factory, stack)
	if err := os.MkdirAll(filepath.Join(dir, "snapshots"), 0o755); err != nil {
		return nil, err
	}
	return &LocalStore{
		Root:    root,
		Factory: factory,
		stack:   stack,
		enc:     enc,
		dir:     dir,
	}, nil
}

// Current returns the snapshot named by the current pointer. Returns
// sdkstate.ErrNoCurrent when no snapshot has been written yet.
func (s *LocalStore) Current() (*sdkstate.Snapshot, error) {
	rev, err := s.currentRev()
	if err != nil {
		return nil, err
	}
	return s.Get(rev)
}

// CurrentRev returns the rev the current pointer names, or sdkstate.ErrNoCurrent.
func (s *LocalStore) CurrentRev() (string, error) {
	return s.currentRev()
}

// Write commits snap to disk and returns its rev. The caller advances
// the current pointer with SetCurrent. Each rev starts as an
// RFC3339Nano timestamp; if a snapshot already exists at that path
// (because two writes share the same nanosecond), a numeric suffix
// is appended until the path is fresh, so uniqueness does not depend
// on the clock advancing between writes.
func (s *LocalStore) Write(snap *sdkstate.Snapshot) (string, error) {
	body, err := sdkstate.EncodeSnapshot(snap)
	if err != nil {
		return "", err
	}
	sealed, err := sdkstate.Seal(body, nil, s.enc)
	if err != nil {
		return "", err
	}
	base := now().UTC().Format(time.RFC3339Nano)
	rev := base
	for attempt := range maxRevAttempts {
		if attempt > 0 {
			rev = fmt.Sprintf("%s_%d", base, attempt)
		}
		path := s.snapshotPath(rev)
		_, statErr := os.Stat(path)
		if statErr == nil {
			continue
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return "", statErr
		}
		if err := ufs.WriteFileAtomic(path, sealed, 0o600); err != nil {
			return "", err
		}
		return rev, nil
	}
	return "", fmt.Errorf("local store: could not allocate fresh revision after %d attempts",
		maxRevAttempts)
}

// Lock acquires the stack's exclusive lock by creating a marker
// file under the stack directory. Lock blocks until the marker
// can be created or ctx is canceled. The marker file holds the
// holder's pid so an operator can identify a stuck lock.
func (s *LocalStore) Lock(ctx context.Context) (sdkstate.Lock, error) {
	path := filepath.Join(s.dir, "lock")
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			if cerr := f.Close(); cerr != nil {
				_ = os.Remove(path)
				return nil, cerr
			}
			return &fileLock{path: path}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// ForceUnlock removes the lock marker without checking who holds it.
// Operators run this to recover after a leaked lock and must ensure
// no concurrent run is in progress.
func (s *LocalStore) ForceUnlock() error {
	err := os.Remove(filepath.Join(s.dir, "lock"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

type fileLock struct {
	path string
}

func (l *fileLock) Unlock() error {
	err := os.Remove(l.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// SetCurrent atomically points "current" at the named rev. The snapshot
// must already exist.
func (s *LocalStore) SetCurrent(rev string) error {
	if _, err := os.Stat(s.snapshotPath(rev)); err != nil {
		return fmt.Errorf("set-current %s: %w", rev, err)
	}
	return ufs.WriteFileAtomic(filepath.Join(s.dir, "current"), []byte(rev+"\n"), 0o600)
}

// Get returns the snapshot with the given rev.
func (s *LocalStore) Get(rev string) (*sdkstate.Snapshot, error) {
	sealed, err := os.ReadFile(s.snapshotPath(rev))
	if err != nil {
		return nil, err
	}
	body, err := sdkstate.Open(sealed, func(*sdkstate.Ref) (sdkencrypt.Encrypter, error) {
		return s.enc, nil
	})
	if err != nil {
		return nil, fmt.Errorf("local store: open %s: %w", rev, err)
	}
	return sdkstate.DecodeSnapshot(body)
}

// List returns the revs of every stored snapshot in chronological order.
func (s *LocalStore) List() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "snapshots"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	const suffix = ".json.enc"
	var out []string
	for _, e := range entries {
		name := e.Name()
		if before, ok := strings.CutSuffix(name, suffix); ok {
			out = append(out, before)
		}
	}
	return out, nil
}

// Delete removes the snapshot with the given rev. Removing a rev that
// does not exist is not an error.
func (s *LocalStore) Delete(rev string) error {
	err := os.Remove(s.snapshotPath(rev))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (s *LocalStore) snapshotPath(rev string) string {
	return filepath.Join(s.dir, "snapshots", rev+".json.enc")
}

func (s *LocalStore) currentRev() (string, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, "current"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", sdkstate.ErrNoCurrent
		}
		return "", err
	}
	rev := strings.TrimSpace(string(b))
	if rev == "" {
		return "", sdkstate.ErrNoCurrent
	}
	return rev, nil
}
