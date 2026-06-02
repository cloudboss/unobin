// Package deps models a factory's dependencies. It reads the
// unobin.manifest, which lists each direct dependency and the lowest
// version the factory accepts for it, and drives pkg/resolve to fetch
// them.
package deps

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/resolve"
)

// Dependency identifies an importable unit by its repository URL and an
// optional subdirectory within that repository. It is the manifest's
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
