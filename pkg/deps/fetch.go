package deps

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/cloudboss/unobin/pkg/projectmarker"
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
	ref := &resolve.RemoteImport{
		URL:     dep.URL,
		Subdir:  dep.Subdir,
		Version: dep.Tag(version),
	}
	src, err := f.resolver.Resolve(ref)
	if err != nil {
		return nil, err
	}
	manifest, err := ReadManifest(src.FS)
	if errors.Is(err, fs.ErrNotExist) {
		if ok, markerErr := HasProjectMarker(src.FS); markerErr != nil {
			return nil, markerErr
		} else if !ok {
			return nil, noProjectMarkerError(dep)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

// RequireProject checks that dep resolves to a project root at version.
func RequireProject(dep Dependency, version string, resolver resolve.Resolver) error {
	ref := &resolve.RemoteImport{URL: dep.URL, Subdir: dep.Subdir, Version: dep.Tag(version)}
	src, err := resolver.Resolve(ref)
	if err != nil {
		return err
	}
	ok, err := HasProjectMarker(src.FS)
	if err != nil {
		return err
	}
	if !ok {
		return noProjectMarkerError(dep)
	}
	return nil
}

func noProjectMarkerError(dep Dependency) error {
	return fmt.Errorf(
		"%s has no manifest.ub or go.mod; deps get operates on projects, "+
			"while .ub imports may name packages below projects",
		dep)
}

// HasProjectMarker reports whether fsys contains manifest.ub or go.mod at its root.
func HasProjectMarker(fsys fs.FS) (bool, error) {
	marker, err := projectmarker.ClassifyRoot(fsys)
	if err != nil {
		return false, err
	}
	return marker.Kind != projectmarker.None, nil
}
