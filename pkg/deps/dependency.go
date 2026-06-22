// Package deps models a factory's dependencies. It reads project.ub,
// which lists each direct dependency and the lowest version the factory
// accepts for it, and drives pkg/resolve to fetch them.
package deps

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/resolve"
)

// Dependency identifies an importable unit by its repository URL and an
// optional subdirectory within that repository. It is the project's
// notion of "a dependency"; a resolved version (a git tag) is paired
// with it elsewhere, not stored on the identity.
type Dependency struct {
	URL    string
	Subdir string
}

// ParseDependency parses a dependency id of the form `repo-url` or
// `repo-url//subdir`, the same `//` separator imports use but without a
// trailing `@version`.
func ParseDependency(id string) (Dependency, error) {
	url, subdir, err := resolve.SplitRepoSubdir(id)
	if err != nil {
		return Dependency{}, err
	}
	if !strings.ContainsRune(url, '/') {
		return Dependency{}, fmt.Errorf(
			"dependency %q: repo URL must contain a host and a path", id)
	}
	return Dependency{URL: url, Subdir: subdir}, nil
}

// String renders the dependency back to its id form, the inverse of
// ParseDependency.
func (d Dependency) String() string {
	if d.Subdir == "" {
		return d.URL
	}
	return d.URL + "//" + d.Subdir
}

// Replacement describes the local path selected for a dependency.
type Replacement struct {
	Dep    Dependency
	Path   string
	Suffix string
	Exact  bool
}

// ReplacementFor returns the local replacement for dep. A repository-level
// replacement covers imports from subdirectories in that repository. A
// subdirectory replacement covers that subdirectory and packages below it.
func ReplacementFor(replace map[Dependency]string, dep Dependency) (Replacement, bool) {
	var best Replacement
	bestLen := -1
	for replDep, path := range replace {
		if replDep.URL != dep.URL {
			continue
		}
		match, ok := replacementMatch(replDep, path, dep)
		if !ok {
			continue
		}
		if n := len(replDep.Subdir); n > bestLen {
			best = match
			bestLen = n
		}
	}
	return best, bestLen >= 0
}

func replacementMatch(replDep Dependency, path string, dep Dependency) (Replacement, bool) {
	if replDep.Subdir == "" {
		return Replacement{Dep: replDep, Path: path, Suffix: dep.Subdir}, true
	}
	if dep.Subdir == replDep.Subdir {
		return Replacement{Dep: replDep, Path: path, Exact: true}, true
	}
	prefix := replDep.Subdir + "/"
	if after, ok := strings.CutPrefix(dep.Subdir, prefix); ok {
		return Replacement{
			Dep:    replDep,
			Path:   path,
			Suffix: after,
			Exact:  true,
		}, true
	}
	return Replacement{}, false
}

// ReplacementPath returns the local replacement path for dep.
func ReplacementPath(replace map[Dependency]string, dep Dependency) (string, bool) {
	match, ok := ReplacementFor(replace, dep)
	if !ok {
		return "", false
	}
	return match.Path, true
}
