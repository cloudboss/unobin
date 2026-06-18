package deps

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/resolve"
)

// ImportedPackages scans every .ub file under root and returns the set of
// packages named by remote imports. The version on an import is not read here:
// a package's version floor lives on its owning project in manifest.ub, not in
// the import string. Local imports are intra-project and contribute no remote
// package. Hidden directories (a leading dot, such as .git) are skipped.
func ImportedPackages(root string) (map[RemotePackage]bool, error) {
	packages := map[RemotePackage]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			hasManifest, err := hasManifestFile(path)
			if err != nil {
				return err
			}
			if hasManifest {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".ub") {
			return nil
		}
		return scanImports(path, packages)
	})
	if err != nil {
		return nil, err
	}
	return packages, nil
}

// ImportedRepos is kept for callers that still need the import strings in the
// older Dependency form.
func ImportedRepos(root string) (map[Dependency]bool, error) {
	packages, err := ImportedPackages(root)
	if err != nil {
		return nil, err
	}
	repos := make(map[Dependency]bool, len(packages))
	for pkg := range packages {
		repos[pkg.Dependency()] = true
	}
	return repos, nil
}

func scanImports(path string, packages map[RemotePackage]bool) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	refs, err := extractSyntaxImportRefs(path, b)
	if err != nil {
		return err
	}
	addSyntaxImportRefs(packages, refs)
	return nil
}

func addSyntaxImportRefs(packages map[RemotePackage]bool, refs []resolve.SyntaxImport) {
	for _, ref := range refs {
		rem, ok := ref.Ref.(*resolve.RemoteImport)
		if !ok {
			continue
		}
		packages[RemotePackage{URL: rem.URL, Subdir: rem.Subdir}] = true
	}
}
