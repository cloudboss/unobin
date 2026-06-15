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
// read here: a repository's version floor lives in manifest.ub, not in
// the import string. Local imports are intra-project and contribute no
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
	addImportRefs(repos, refs)
	if !hasSourceDeclaredImports(f) {
		return nil
	}
	srefs, err := extractSyntaxImportRefs(f)
	if err != nil {
		return err
	}
	addSyntaxImportRefs(repos, srefs)
	return nil
}

func addImportRefs(repos map[Dependency]bool, refs map[string]resolve.ImportRef) {
	for _, ref := range refs {
		rem, ok := ref.(*resolve.RemoteImport)
		if !ok {
			continue
		}
		repos[Dependency{URL: rem.URL}] = true
	}
}

func addSyntaxImportRefs(repos map[Dependency]bool, refs []resolve.SyntaxImport) {
	for _, ref := range refs {
		rem, ok := ref.Ref.(*resolve.RemoteImport)
		if !ok {
			continue
		}
		repos[Dependency{URL: rem.URL}] = true
	}
}

func hasSourceDeclaredImports(f *lang.File) bool {
	if f == nil || f.Body == nil {
		return false
	}
	switch filepath.Base(f.Path) {
	case "factory.ub", "manifest.ub", "lock.ub":
		return true
	}
	for _, fld := range f.Body.Fields {
		if fld.Decl != nil {
			return true
		}
	}
	return len(f.Body.Fields) == 1 && isSourceDeclaredRole(f.Body.Fields[0])
}

func isSourceDeclaredRole(fld *lang.Field) bool {
	if fld.Key.Kind != lang.FieldIdent {
		return false
	}
	switch fld.Key.Name {
	case "factory", "stack", "manifest", "lock":
		return true
	default:
		return false
	}
}
