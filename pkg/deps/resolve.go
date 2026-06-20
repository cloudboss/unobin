package deps

import "fmt"

// Fetcher fetches a dependency's manifest at a selected version, for the
// version walk. It returns nil when the dependency declares no manifest:
// a leaf with no further dependencies, such as a Go library or a UB
// library that imports nothing remote.
type Fetcher interface {
	Fetch(dep Dependency, version string) (*Manifest, error)
}

// Resolve runs minimal version selection over the dependency graph rooted
// at a manifest. It selects the highest floor for each dependency, fetches
// it at that version to read its own requirements, and repeats until the
// selection stops changing. A dependency whose version is later raised is
// re-fetched, since a higher version may declare different requirements;
// the walk terminates at any dependency with no manifest. The result maps
// each dependency to its selected version -- the lock, keyed per imported
// library, is built separately by the import walk.
func Resolve(root *Manifest, fetch Fetcher) (map[Dependency]string, error) {
	sel := NewSelection()
	var queue []Dependency
	enqueue := func(reqs map[Dependency]string) {
		for dep, floor := range reqs {
			if sel.Add(dep, floor) {
				queue = append(queue, dep)
			}
		}
	}
	enqueue(root.RequireVersions())

	fetchedAt := map[Dependency]string{}
	for len(queue) > 0 {
		dep := queue[0]
		queue = queue[1:]
		version := sel.Version(dep)
		if fetchedAt[dep] == version {
			continue // already fetched at the current selection
		}
		manifest, err := fetch.Fetch(dep, version)
		if err != nil {
			return nil, fmt.Errorf("resolve %s@%s: %w", dep, version, err)
		}
		fetchedAt[dep] = version
		if manifest != nil {
			enqueue(manifest.RequireVersions())
		}
	}
	return sel.Chosen(), nil
}
