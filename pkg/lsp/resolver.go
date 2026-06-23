package lsp

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/resolve"
)

type cachedRemoteSource interface {
	CachedSource(ref *resolve.RemoteImport, commit string) (*resolve.Source, bool, error)
}

// ImportResolver resolves LSP imports without network access.
type ImportResolver struct {
	root    string
	project *deps.Project
	lock    *deps.ProjectLock
	local   *resolve.LocalResolver
	remote  cachedRemoteSource
}

// NewImportResolver returns an import resolver for one project root.
func NewImportResolver(
	root string,
	project *deps.Project,
	lock *deps.ProjectLock,
	remote cachedRemoteSource,
) *ImportResolver {
	return &ImportResolver{
		root:    root,
		project: project,
		lock:    lock,
		local:   resolve.NewLocalResolver(root),
		remote:  remote,
	}
}

// Resolve resolves ref or returns an error when no cached source exists.
func (r *ImportResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	src, ok, err := r.ResolveNoFetch(ref)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("import source is not cached: %w", fs.ErrNotExist)
	}
	return src, nil
}

// ResolveNoFetch resolves ref from local files or cached remote data only.
func (r *ImportResolver) ResolveNoFetch(
	ref resolve.ImportRef,
) (*resolve.Source, bool, error) {
	switch v := ref.(type) {
	case *resolve.LocalImport:
		src, err := r.local.Resolve(v)
		if err != nil {
			return nil, false, err
		}
		return src, true, nil
	case *resolve.RemoteImport:
		if src, ok, err := r.resolveReplacement(v); ok || err != nil {
			return src, ok, err
		}
		return r.resolveCachedRemote(v)
	default:
		return nil, false, fmt.Errorf("unsupported import ref %T", ref)
	}
}

func (r *ImportResolver) resolveReplacement(
	ref *resolve.RemoteImport,
) (*resolve.Source, bool, error) {
	if r.project == nil || len(r.project.Replace) == 0 {
		return nil, false, nil
	}
	dep := deps.Dependency{URL: ref.URL, Subdir: ref.Subdir}
	replacement, ok := deps.ReplacementFor(r.project.Replace, dep)
	if !ok {
		return nil, false, nil
	}
	path := replacement.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.root, path)
	}
	if replacement.Suffix != "" {
		path = filepath.Join(path, replacement.Suffix)
	}
	src, err := resolve.NewLocalResolver(path).Resolve(&resolve.LocalImport{Path: "."})
	if err != nil {
		return nil, false, err
	}
	return src, true, nil
}

func (r *ImportResolver) resolveCachedRemote(
	ref *resolve.RemoteImport,
) (*resolve.Source, bool, error) {
	if r.lock == nil || r.remote == nil {
		return nil, false, nil
	}
	dep := deps.Dependency{URL: ref.URL, Subdir: ref.Subdir}
	entry, ok := r.lock.Deps[dep.String()]
	if !ok {
		return nil, false, nil
	}
	cachedRef := *ref
	cachedRef.Version = entry.Version
	return r.remote.CachedSource(&cachedRef, entry.Commit)
}
