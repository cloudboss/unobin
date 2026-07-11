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
	before, err := snapshotFile(path)
	if err != nil {
		return Change{}, err
	}
	if before.exists && before.kind.IsRegular() && bytes.Equal(before.content, content) {
		return Change{Path: path, Action: ActionUnchanged}, nil
	}
	writeErr := ufs.WriteFileAtomic(path, content, mode)
	after, observeErr := snapshotFile(path)
	if observeErr != nil {
		return Change{}, errors.Join(writeErr, observeErr)
	}
	change := observedChange(path, before, after)
	return change, writeErr
}

type fileSnapshot struct {
	exists  bool
	kind    fs.FileMode
	content []byte
}

func snapshotFile(path string) (fileSnapshot, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fileSnapshot{}, nil
	}
	if err != nil {
		return fileSnapshot{}, err
	}
	snapshot := fileSnapshot{exists: true, kind: info.Mode().Type()}
	switch {
	case info.Mode().IsRegular():
		snapshot.content, err = os.ReadFile(path)
	case info.Mode()&os.ModeSymlink != 0:
		var target string
		target, err = os.Readlink(path)
		snapshot.content = []byte(target)
	}
	return snapshot, err
}

func observedChange(path string, before, after fileSnapshot) Change {
	switch {
	case !before.exists && after.exists:
		return Change{Path: path, Action: ActionCreated}
	case before.exists && !after.exists:
		return Change{Path: path, Action: ActionRemoved}
	case before.exists && after.exists:
		action := ActionUpdated
		if before.kind == after.kind && bytes.Equal(before.content, after.content) {
			action = ActionUnchanged
		}
		return Change{Path: path, Action: action}
	default:
		return Change{}
	}
}
