package deps

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// TagPrefix returns the most specific git tag prefix for this dependency.
func (d Dependency) TagPrefix() string {
	if d.Subdir == "" {
		return ""
	}
	return d.Subdir + "/"
}

// Tag returns the most specific git tag that names a version of this dependency.
func (d Dependency) Tag(version string) string {
	return d.TagPrefix() + version
}

// Versions returns the dependency's semver tags in increasing order.
func Versions(dep Dependency, tags []string) []string {
	for _, prefix := range tagPrefixes(dep.Subdir) {
		versions := versionsWithPrefix(prefix, tags)
		if len(versions) > 0 {
			return versions
		}
	}
	return nil
}

func tagPrefixes(subdir string) []string {
	if subdir == "" {
		return []string{""}
	}
	var out []string
	for s := subdir; s != ""; s = parentSubdir(s) {
		out = append(out, s+"/")
	}
	out = append(out, "")
	return out
}

func parentSubdir(subdir string) string {
	idx := strings.LastIndex(subdir, "/")
	if idx < 0 {
		return ""
	}
	return subdir[:idx]
}

func versionsWithPrefix(prefix string, tags []string) []string {
	var out []string
	for _, t := range tags {
		version, ok := strings.CutPrefix(t, prefix)
		if !ok || !semver.IsValid(version) {
			continue
		}
		out = append(out, version)
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

// ResolveVersion turns a `deps get` query into a concrete version of dep,
// chosen among the repository's tags. An empty query or "latest" picks the
// highest available version; a full version (vX.Y.Z) is used as-is once
// confirmed present; a partial version (v1 or v1.2) picks the highest
// available version under that prefix.
func ResolveVersion(dep Dependency, query string, tags []string) (string, error) {
	available := Versions(dep, tags)
	if query == "" || query == "latest" {
		if v := Highest(available); v != "" {
			return v, nil
		}
		return "", fmt.Errorf("%s: no versions available", dep)
	}
	if !semver.IsValid(query) {
		return "", fmt.Errorf("%s: %q is not a version", dep, query)
	}
	if semver.Canonical(query) == query {
		for _, v := range available {
			if v == query {
				return v, nil
			}
		}
		return "", fmt.Errorf("%s: version %s is not available", dep, query)
	}
	var matches []string
	for _, v := range available {
		if v == query || strings.HasPrefix(v, query+".") {
			matches = append(matches, v)
		}
	}
	if v := Highest(matches); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s: no version matches %s", dep, query)
}
