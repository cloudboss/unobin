package deps

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	pathpkg "path"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/projectmarker"
)

func HashUBProject(fsys fs.FS) (string, error) {
	if fsys == nil {
		return "", fmt.Errorf("hash UB project: missing filesystem")
	}
	paths, err := ubProjectHashPaths(fsys)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, p := range paths {
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\n%d\n", p, len(body))
		h.Write(body)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func ubProjectHashPaths(fsys fs.FS) ([]string, error) {
	var paths []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlink is not supported", p)
		}
		if hiddenPath(p) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			marker, err := projectmarker.Classify(fsys, p)
			if err != nil {
				return err
			}
			if marker.Kind != projectmarker.None {
				return fs.SkipDir
			}
			return nil
		}
		if pathpkg.Base(p) == SourceLockFileName || !strings.HasSuffix(p, ".ub") {
			return nil
		}
		include, err := includeUBHashFile(fsys, p)
		if err != nil {
			return err
		}
		if include {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	return paths, nil
}

func hiddenPath(p string) bool {
	for part := range strings.SplitSeq(p, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func includeUBHashFile(fsys fs.FS, p string) (bool, error) {
	body, err := fs.ReadFile(fsys, p)
	if err != nil {
		return false, err
	}
	file, err := syntax.ParseSource(p, body)
	if err != nil {
		return false, fmt.Errorf("%s: %w", p, err)
	}
	return file.Kind != syntax.FileStack, nil
}
