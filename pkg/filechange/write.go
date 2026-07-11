package filechange

import (
	"bytes"
	"errors"
	"io/fs"
	"os"

	ufs "github.com/cloudboss/unobin/pkg/fs"
)

// WriteFile atomically writes content and reports the resulting file action.
func WriteFile(path string, content []byte, mode fs.FileMode) (Change, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := ufs.WriteFileAtomic(path, content, mode); err != nil {
			return Change{}, err
		}
		return Change{Path: path, Action: ActionCreated}, nil
	}
	if err != nil {
		return Change{}, err
	}
	if info.Mode().IsRegular() {
		existing, err := os.ReadFile(path)
		if err != nil {
			return Change{}, err
		}
		if bytes.Equal(existing, content) {
			return Change{Path: path, Action: ActionUnchanged}, nil
		}
	}
	if err := ufs.WriteFileAtomic(path, content, mode); err != nil {
		return Change{}, err
	}
	return Change{Path: path, Action: ActionUpdated}, nil
}
