package deps

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/resolve"
)

// ImportedRepos scans every .ub file under root and returns the set of
// repositories named by remote imports. The version on an import is not
// read here: a repository's version floor lives in unobin.manifest, not
// in the import string. Local imports are intra-project and contribute no
// repository. Hidden directories (a leading dot, such as .git) are
// skipped.
func ImportedRepos(root string) (map[Dependency]bool, error) {
	repos := map[Dependency]bool{}
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
		return scanImports(path, repos)
	})
	if err != nil {
		return nil, err
	}
	return repos, nil
}

func scanImports(path string, repos map[Dependency]bool) error {
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
	for _, ref := range refs {
		rem, ok := ref.(*resolve.RemoteImport)
		if !ok {
			continue // local imports are intra-project
		}
		repos[Dependency{URL: rem.URL}] = true
	}
	return nil
}
