package codegen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ContentRevision returns a short content-addressable revision for the
// generated library in dir. It hashes every Go source file plus go.mod and
// go.sum in sorted path order, so the result is a stable fingerprint of the
// factory source, the inlined UB libraries, and the full pinned Go dependency
// set that go.sum records. The compiled binary itself is excluded; only
// build inputs contribute. Run it after `go mod tidy` so go.sum is present.
func ContentRevision(dir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == "go.mod" || name == "go.sum" || strings.HasSuffix(name, ".go") {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	slices.Sort(paths)

	h := sha256.New()
	for _, rel := range paths {
		body, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\n%d\n", filepath.ToSlash(rel), len(body))
		h.Write(body)
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}
