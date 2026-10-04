package deps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func ResolveSelectedModule(
	importPath string,
	selection map[Dependency]string,
	resolver resolve.Resolver,
	replace map[Dependency]string,
) (golibrary.ModuleSource, error) {
	var selected Dependency
	var prefix string
	for _, projects := range []map[Dependency]string{selection, replace} {
		for dep := range projects {
			candidate := dep.URL
			if dep.Subdir != "" {
				candidate += "/" + dep.Subdir
			}
			if importPath != candidate && !strings.HasPrefix(importPath, candidate+"/") {
				continue
			}
			if len(candidate) > len(prefix) || len(candidate) == len(prefix) &&
				dep.String() < selected.String() {
				selected, prefix = dep, candidate
			}
		}
	}
	if prefix == "" {
		return golibrary.ModuleSource{}, nil
	}
	version := selection[selected]
	_, replaced := ReplacementFor(replace, selected)
	replaced = replaced || IsReplacementSentinel(version)
	ref := remoteProjectRef(ProjectID(selected), version)
	if replaced {
		ref.Version = ""
	}
	source, err := resolver.Resolve(ref)
	if err != nil {
		return golibrary.ModuleSource{}, fmt.Errorf("read selected module %s: %w", selected, err)
	}
	if source == nil || source.Path == "" {
		return golibrary.ModuleSource{}, nil
	}
	if _, err := os.Stat(filepath.Join(source.Path, "go.mod")); err != nil {
		return golibrary.ModuleSource{}, fmt.Errorf("read selected module %s: %w", selected, err)
	}
	module, err := golibrary.ModuleSourceAt(source.Path)
	if err != nil {
		return golibrary.ModuleSource{}, err
	}
	if !replaced {
		if err := resolve.ValidateGoModulePath(ref, module.Path); err != nil {
			return golibrary.ModuleSource{}, err
		}
	}
	module.Dependency, module.Version, module.Commit = selected.String(), version, source.Commit
	if replaced {
		module.Replacement = module.Dir
		if module.Version == "" {
			module.Version = ReplacementSentinel
		}
	}
	return module, nil
}
