package deps

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/projectmarker"
)

// FindProjectDir walks up from start to the nearest ancestor directory
// holding a project and returns that directory: the root of the project that
// governs start. start may be any path inside the project, a directory or a
// file. It stops at the filesystem root and returns an error wrapping
// fs.ErrNotExist when no project is found.
func FindProjectDir(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		ok, err := hasProjectFile(dir)
		if err != nil {
			return "", err
		}
		if ok {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf(
				"no %s found in %s or any parent directory: %w",
				ProjectFileName,
				start,
				fs.ErrNotExist,
			)
		}
		dir = parent
	}
}

func FindProjectMarkerDir(start string) (string, projectmarker.Marker, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", projectmarker.Marker{}, err
	}
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			dir = filepath.Dir(dir)
		}
	} else if errors.Is(err, fs.ErrNotExist) {
		dir = filepath.Dir(dir)
	} else {
		return "", projectmarker.Marker{}, err
	}
	for {
		marker, err := projectmarker.ClassifyDir(dir)
		if err != nil {
			if unreadableDirectory(dir, err) {
				return "", projectmarker.Marker{}, projectMarkerNotFoundError(start)
			}
			return "", projectmarker.Marker{}, err
		}
		if marker.Kind != projectmarker.None {
			return dir, marker, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", projectmarker.Marker{}, projectMarkerNotFoundError(start)
		}
		dir = parent
	}
}

func projectMarkerNotFoundError(start string) error {
	return fmt.Errorf(
		"no project.ub or go.mod found in %s or any parent directory: %w",
		start,
		fs.ErrNotExist,
	)
}

func unreadableDirectory(dir string, err error) bool {
	if !errors.Is(err, fs.ErrPermission) {
		return false
	}
	_, readErr := os.ReadDir(dir)
	return errors.Is(readErr, fs.ErrPermission)
}

func hasProjectFile(dir string) (bool, error) {
	candidate := filepath.Join(dir, ProjectFileName)
	info, err := os.Stat(candidate)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}
	b, err := os.ReadFile(candidate)
	if err != nil {
		return false, err
	}
	if err := validateProjectSource(ProjectFileName, b); err != nil {
		return false, err
	}
	return true, nil
}
