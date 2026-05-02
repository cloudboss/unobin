package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	ufs "github.com/cloudboss/unobin/pkg/fs"
)

const maxRevAttempts = 100

// ErrNoCurrent is returned by LocalStore.Current when no snapshot has been
// written for this deployment.
var ErrNoCurrent = errors.New("no current snapshot")

// LocalStore reads and writes snapshots under a per-deployment directory.
// Layout is as follows:
//
//	<Root>/<Stack>/<DeploymentID>/
//	  current             // File containing the SHA of the current snapshot.
//	  snapshots/
//	    <rev>.json.enc    // rev is an RFC3339Nano timestamp
//	    ...
type LocalStore struct {
	Root         string
	Stack        string
	DeploymentID string

	enc Encrypter
	dir string
}

// NewLocalStore returns a LocalStore for the given stack and deployment ID
// under root, creating the directory tree if it doesn't exist. The
// Encrypter is required, but NoopEncrypter{} can be passed for tests.
func NewLocalStore(root, stack, deploymentID string, enc Encrypter) (*LocalStore, error) {
	if stack == "" {
		return nil, errors.New("local store: stack is required")
	}
	if deploymentID == "" {
		return nil, errors.New("local store: deployment-id is required")
	}
	if enc == nil {
		return nil, errors.New("local store: encrypter is required")
	}
	dir := filepath.Join(root, stack, deploymentID)
	if err := os.MkdirAll(filepath.Join(dir, "snapshots"), 0o755); err != nil {
		return nil, err
	}
	return &LocalStore{
		Root:         root,
		Stack:        stack,
		DeploymentID: deploymentID,
		enc:          enc,
		dir:          dir,
	}, nil
}

// Current returns the snapshot named by the current pointer. Returns
// ErrNoCurrent when no snapshot has been written yet.
func (s *LocalStore) Current() (*Snapshot, error) {
	rev, err := s.currentRev()
	if err != nil {
		return nil, err
	}
	return s.Get(rev)
}

// CurrentRev returns the rev the current pointer names, or ErrNoCurrent.
func (s *LocalStore) CurrentRev() (string, error) {
	return s.currentRev()
}

// Write commits snap to disk and returns its rev (an RFC3339Nano
// timestamp). The caller advances the current pointer with SetCurrent.
// On the rare collision where two writes land in the same nanosecond,
// Write retries with a fresh timestamp.
func (s *LocalStore) Write(snap *Snapshot) (string, error) {
	plaintext, err := EncodeSnapshot(snap)
	if err != nil {
		return "", err
	}
	ciphertext, err := s.enc.Encrypt(plaintext)
	if err != nil {
		return "", err
	}
	for attempt := 0; attempt < maxRevAttempts; attempt++ {
		rev := time.Now().UTC().Format(time.RFC3339Nano)
		path := s.snapshotPath(rev)
		_, statErr := os.Stat(path)
		if statErr == nil {
			time.Sleep(time.Nanosecond)
			continue
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return "", statErr
		}
		if err := ufs.WriteFileAtomic(path, ciphertext, 0o600); err != nil {
			return "", err
		}
		return rev, nil
	}
	return "", fmt.Errorf("local store: could not allocate fresh revision after %d attempts", maxRevAttempts)
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
func (s *LocalStore) Get(rev string) (*Snapshot, error) {
	ciphertext, err := os.ReadFile(s.snapshotPath(rev))
	if err != nil {
		return nil, err
	}
	plaintext, err := s.enc.Decrypt(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("local store: decrypt %s: %w", rev, err)
	}
	return DecodeSnapshot(plaintext)
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
		if strings.HasSuffix(name, suffix) {
			out = append(out, strings.TrimSuffix(name, suffix))
		}
	}
	return out, nil
}

func (s *LocalStore) snapshotPath(rev string) string {
	return filepath.Join(s.dir, "snapshots", rev+".json.enc")
}

func (s *LocalStore) currentRev() (string, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, "current"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNoCurrent
		}
		return "", err
	}
	rev := strings.TrimSpace(string(b))
	if rev == "" {
		return "", ErrNoCurrent
	}
	return rev, nil
}

