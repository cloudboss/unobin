package deps

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/resolve"
)

// Resolved is one dependency's fetch result: where it was fetched, its own
// manifest (nil when it declares none, as a Go library does), and how its
// integrity is tracked. The pinned commit and content hash come from the
// Source.
type Resolved struct {
	Source   *resolve.Source
	Manifest *Manifest
	Kind     LockKind
}

// Fetcher fetches a dependency at a selected version. The walk calls it
// once per distinct (dependency, version) it needs.
type Fetcher interface {
	Fetch(dep Dependency, version string) (Resolved, error)
}

// Result is the outcome of resolving a root manifest: the pinned Lock and
// the per-dependency fetch results a consumer builds from.
type Result struct {
	Lock *Lock
	Deps map[Dependency]Resolved
}

// Resolve computes the full pinned set for root by minimal version
// selection. It selects the highest floor for each dependency, fetches it
// at that version to read its own requirements, and repeats until the
// selection stops changing. A dependency whose version is later raised is
// re-fetched, because a higher version may declare different requirements.
// The walk terminates at any dependency that declares no manifest.
func Resolve(root *Manifest, fetch Fetcher) (*Result, error) {
	sel := NewSelection()
	var queue []Dependency
	enqueue := func(reqs map[Dependency]string) {
		for dep, floor := range reqs {
			if sel.Add(dep, floor) {
				queue = append(queue, dep)
			}
		}
	}
	enqueue(root.Requires)

	resolved := map[Dependency]Resolved{}
	fetchedAt := map[Dependency]string{}
	for len(queue) > 0 {
		dep := queue[0]
		queue = queue[1:]
		version := sel.Version(dep)
		if fetchedAt[dep] == version {
			continue // already fetched at the current selection
		}
		res, err := fetch.Fetch(dep, version)
		if err != nil {
			return nil, fmt.Errorf("resolve %s@%s: %w", dep, version, err)
		}
		fetchedAt[dep] = version
		resolved[dep] = res
		if res.Manifest != nil {
			enqueue(res.Manifest.Requires)
		}
	}

	lock := NewLock()
	for dep, res := range resolved {
		var commit, hash string
		if res.Source != nil {
			commit, hash = res.Source.Commit, res.Source.Hash
		}
		lock.Deps[dep.String()] = &LockedDep{
			Kind:    res.Kind,
			Version: sel.Version(dep),
			Commit:  commit,
			Hash:    hash,
		}
	}
	if err := validateLockedDeps(lock); err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}
	return &Result{Lock: lock, Deps: resolved}, nil
}
