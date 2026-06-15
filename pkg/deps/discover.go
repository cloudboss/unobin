package deps

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FindManifestDir walks up from start to the nearest ancestor directory
// holding a manifest and returns that directory: the root of the project that
// governs start. start may be any path inside the project, a directory or a
// file. It stops at the filesystem root and returns an error wrapping
// fs.ErrNotExist when no manifest is found.
func FindManifestDir(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if hasManifestFile(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf(
				"no %s found in %s or any parent directory: %w",
				ManifestFileName,
				start,
				fs.ErrNotExist,
			)
		}
		dir = parent
	}
}

func hasManifestFile(dir string) bool {
	candidate := filepath.Join(dir, ManifestFileName)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return true
	}
	return false
}
