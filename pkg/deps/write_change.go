package deps

import (
	"bytes"
	"errors"
	"io/fs"
	"os"

	"github.com/cloudboss/unobin/pkg/filechange"
)

func pendingFileChange(path string, content []byte) (filechange.Change, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return filechange.Change{Path: path, Action: filechange.ActionCreated}, true, nil
	}
	if err != nil {
		return filechange.Change{}, false, err
	}
	change := filechange.Change{Path: path, Action: filechange.ActionUpdated}
	if !info.Mode().IsRegular() {
		return change, true, nil
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return filechange.Change{}, false, err
	}
	if bytes.Equal(existing, content) {
		change.Action = filechange.ActionUnchanged
		return change, false, nil
	}
	return change, true, nil
}
