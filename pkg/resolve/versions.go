package resolve

import (
	"fmt"
	"sort"
)

// CheckSameRepoVersions returns one error per repo URL where two remote
// imports disagree on version. Local imports are ignored. Results are
// sorted by URL so callers see deterministic output.
//
// Same-repo imports must share a version because the underlying repo is
// fetched once: a single resolved commit must satisfy every alias that
// points into it.
func CheckSameRepoVersions(refs map[string]ImportRef) []error {
	type seenEntry struct {
		version string
		alias   string
	}
	seen := make(map[string]seenEntry)
	conflicts := make(map[string][]error)

	aliases := make([]string, 0, len(refs))
	for alias := range refs {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)

	for _, alias := range aliases {
		rem, ok := refs[alias].(*RemoteImport)
		if !ok {
			continue
		}
		if prior, exists := seen[rem.URL]; exists {
			if prior.version != rem.Version {
				conflicts[rem.URL] = append(conflicts[rem.URL],
					fmt.Errorf(
						"import %q at %q conflicts with import %q at %q (same repo %q)",
						alias, rem.Version, prior.alias, prior.version, rem.URL))
			}
			continue
		}
		seen[rem.URL] = seenEntry{version: rem.Version, alias: alias}
	}

	urls := make([]string, 0, len(conflicts))
	for url := range conflicts {
		urls = append(urls, url)
	}
	sort.Strings(urls)
	var out []error
	for _, url := range urls {
		out = append(out, conflicts[url]...)
	}
	return out
}
