package deps

// Fetcher fetches a dependency's project at a selected version, for the
// version walk. It returns nil when the dependency declares no project:
// a leaf with no further dependencies, such as a Go library or a UB
// library that imports nothing remote.
type Fetcher interface {
	Fetch(dep Dependency, version string) (*Project, error)
}

// Resolve runs minimal version selection over the dependency graph rooted
// at a project. It selects the highest floor for each dependency, fetches
// it at that version to read its own requirements, and repeats until the
// selection stops changing. A dependency whose version is later raised is
// re-fetched, since a higher version may declare different requirements;
// the walk terminates at any dependency with no project. The result maps
// each dependency to its selected version -- project-lock, keyed per imported
// library, is built separately by the import walk.
func Resolve(root *Project, fetch Fetcher) (map[Dependency]string, error) {
	result, err := ResolveWithTrace(root, fetch)
	if err != nil {
		return nil, err
	}
	return result.Selection, nil
}
