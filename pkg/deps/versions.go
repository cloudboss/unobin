package deps

import (
	"strings"

	"golang.org/x/mod/semver"
)

// TagPrefix returns the git tag prefix for this dependency. A
// subdirectory dependency uses `<subdir>/` (the monorepo convention Go
// submodules also follow); a repo-root dependency uses no prefix.
func (d Dependency) TagPrefix() string {
	if d.Subdir == "" {
		return ""
	}
	return d.Subdir + "/"
}

// Tag returns the git tag that names a version of this dependency: the
// bare version for a repo-root dependency, or `<subdir>/<version>` for a
// subdirectory dependency.
func (d Dependency) Tag(version string) string {
	return d.TagPrefix() + version
}

// Versions returns the versions of dep found among a repository's tags,
// in increasing semver order. A tag qualifies only when it carries dep's
// prefix and the remainder is a valid semver string, so tags for other
// subdirectories and non-semver tags are ignored. The prefix is stripped:
// each returned element is a bare version, the form a manifest records.
func Versions(dep Dependency, tags []string) []string {
	prefix := dep.TagPrefix()
	var out []string
	for _, t := range tags {
		rest, ok := strings.CutPrefix(t, prefix)
		if !ok || !semver.IsValid(rest) {
			continue
		}
		out = append(out, rest)
	}
	semver.Sort(out)
	return out
}

// Highest returns the greatest version in vs by semver order, or "" when
// vs is empty.
func Highest(vs []string) string {
	best := ""
	for _, v := range vs {
		if best == "" || semver.Compare(v, best) > 0 {
			best = v
		}
	}
	return best
}
