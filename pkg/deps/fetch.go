package deps

import (
	"errors"
	"io/fs"

	"github.com/cloudboss/unobin/pkg/resolve"
)

// manifestFetcher reads dependency manifests through a resolve.Resolver.
// It fetches each project at the git tag for its selected version and
// reads the manifest at the fetched root; a project with no manifest is a
// leaf.
type manifestFetcher struct {
	resolver resolve.Resolver
}

// NewFetcher returns a Fetcher that reads dependency manifests through
// resolver. It is the production Fetcher behind unobin deps; tests pass a
// fake resolver.
func NewFetcher(resolver resolve.Resolver) Fetcher {
	return &manifestFetcher{resolver: resolver}
}

func (f *manifestFetcher) Fetch(dep Dependency, version string) (*Manifest, error) {
	ref := &resolve.RemoteImport{URL: dep.URL, Subdir: dep.Subdir, Version: dep.Tag(version)}
	src, err := f.resolver.Resolve(ref)
	if err != nil {
		return nil, err
	}
	manifest, err := ReadManifest(src.FS)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // a leaf: no manifest means no further dependencies
	}
	if err != nil {
		return nil, err
	}
	return manifest, nil
}
