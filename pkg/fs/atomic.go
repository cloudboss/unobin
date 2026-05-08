// Package fs holds filesystem helpers shared across packages.
package fs

import (
	"errors"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes content to path via a sibling
// `.<basename>.tmp` file: write, fsync, rename into place, fsync the
// parent directory so the rename survives a crash. The parent
// directory must already exist.
func WriteFileAtomic(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if base == "" || base == "." || base == ".." {
		return errors.New("WriteFileAtomic: invalid path")
	}
	tmp := filepath.Join(dir, "."+base+".tmp")

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	parent, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer func() { _ = parent.Close() }()
	_ = parent.Sync()
	return nil
}
