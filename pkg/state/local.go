package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoCurrent is returned by LocalStore.Current when no snapshot has been
// written for this deployment.
var ErrNoCurrent = errors.New("no current snapshot")

// LocalStore reads and writes snapshots under a per-deployment directory.
// Layout is as follows:
//
//	<Root>/<Stack>/<DeploymentID>/
//	  current             // File containing the SHA of the current snapshot.
//	  snapshots/
//	    <sha>.json
//	    ...
type LocalStore struct {
	Root         string
	Stack        string
	DeploymentID string

	dir string
}

// NewLocalStore returns a LocalStore for the given stack and deployment ID
// under root, creating the directory tree if it doesn't exist.
func NewLocalStore(root, stack, deploymentID string) (*LocalStore, error) {
	if stack == "" {
		return nil, errors.New("local store: stack is required")
	}
	if deploymentID == "" {
		return nil, errors.New("local store: deployment-id is required")
	}
	dir := filepath.Join(root, stack, deploymentID)
	if err := os.MkdirAll(filepath.Join(dir, "snapshots"), 0o755); err != nil {
		return nil, err
	}
	return &LocalStore{
		Root:         root,
		Stack:        stack,
		DeploymentID: deploymentID,
		dir:          dir,
	}, nil
}

// Current returns the snapshot named by the current pointer. Returns
// ErrNoCurrent when no snapshot has been written yet.
func (s *LocalStore) Current() (*Snapshot, error) {
	sha, err := s.currentSHA()
	if err != nil {
		return nil, err
	}
	return s.Get(sha)
}

// CurrentSHA returns the SHA the current pointer names, or ErrNoCurrent.
func (s *LocalStore) CurrentSHA() (string, error) {
	return s.currentSHA()
}

// Write commits snap to disk and returns its content SHA. The caller
// advances the current pointer with SetCurrent.
func (s *LocalStore) Write(snap *Snapshot) (string, error) {
	data, err := EncodeSnapshot(snap)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	path := s.snapshotPath(sha)
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return "", err
	}
	return sha, nil
}

// SetCurrent atomically points "current" at the named SHA. The snapshot
// must already exist.
func (s *LocalStore) SetCurrent(sha string) error {
	if _, err := os.Stat(s.snapshotPath(sha)); err != nil {
		return fmt.Errorf("set-current %s: %w", sha, err)
	}
	return writeFileAtomic(filepath.Join(s.dir, "current"), []byte(sha+"\n"), 0o600)
}

// Get returns the snapshot with the given content SHA.
func (s *LocalStore) Get(sha string) (*Snapshot, error) {
	b, err := os.ReadFile(s.snapshotPath(sha))
	if err != nil {
		return nil, err
	}
	return DecodeSnapshot(b)
}

// List returns the SHAs of every stored snapshot.
func (s *LocalStore) List() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "snapshots"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".json") {
			out = append(out, strings.TrimSuffix(name, ".json"))
		}
	}
	return out, nil
}

func (s *LocalStore) snapshotPath(sha string) string {
	return filepath.Join(s.dir, "snapshots", sha+".json")
}

func (s *LocalStore) currentSHA() (string, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, "current"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNoCurrent
		}
		return "", err
	}
	sha := strings.TrimSpace(string(b))
	if sha == "" {
		return "", ErrNoCurrent
	}
	return sha, nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
