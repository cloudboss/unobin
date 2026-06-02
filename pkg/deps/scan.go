package deps

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/resolve"
	"golang.org/x/mod/semver"
)

// ManifestFromImports builds a manifest from the remote imports in a
// project's source. It scans every .ub file under root, groups remote
// imports by repository, and keeps the highest version required for each;
// during migration the inline @version on an import is read as the floor.
// Local imports are ignored -- they are intra-project and need no
// requirement.
func ManifestFromImports(root string) (*Manifest, error) {
	sel := NewSelection()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".ub") {
			return nil
		}
		return scanImports(path, sel)
	})
	if err != nil {
		return nil, err
	}
	return &Manifest{Requires: sel.Chosen()}, nil
}

func scanImports(path string, sel *Selection) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f, err := lang.ParseSource(path, b)
	if err != nil {
		return err
	}
	refs, errs := resolve.ExtractImports(f)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	for alias, ref := range refs {
		rem, ok := ref.(*resolve.RemoteImport)
		if !ok {
			continue // local imports are intra-project
		}
		if !semver.IsValid(rem.Version) {
			return fmt.Errorf("%s: import %q (%s) needs a version tag, got %q",
				path, alias, rem.URL, rem.Version)
		}
		sel.Add(Dependency{URL: rem.URL}, rem.Version)
	}
	return nil
}
