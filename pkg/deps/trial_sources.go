package deps

import (
	"io/fs"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/projectmarker"
	"github.com/cloudboss/unobin/pkg/resolve"
)

type trialSourceKey struct {
	project ProjectID
	version string
}

type trialSource struct {
	ref    resolve.RemoteImport
	source *resolve.Source
}

type trialResolver struct {
	wrapped resolve.Resolver
	first   map[trialSourceKey]trialSource
	sources map[resolve.RemoteImport]*resolve.Source
}

func NewTrialResolver(wrapped resolve.Resolver) resolve.Resolver {
	return &trialResolver{
		wrapped: wrapped, first: map[trialSourceKey]trialSource{},
		sources: map[resolve.RemoteImport]*resolve.Source{},
	}
}

func (r *trialResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	remote, ok := ref.(*resolve.RemoteImport)
	if !ok {
		return r.wrapped.Resolve(ref)
	}
	normalized := *remote
	if normalized.ProjectSubdir == "" && normalized.PackageSubdir == "" {
		normalized.ProjectSubdir = normalized.Subdir
	}
	if normalized.PackageSubdir == "" {
		normalized.PackageSubdir = normalized.Subdir
	}
	if source, found := r.sources[normalized]; found {
		return source, nil
	}
	key := trialSourceKey{
		project: ProjectID{URL: normalized.URL, Subdir: normalized.ProjectSubdir},
		version: normalized.Version,
	}
	first, frozen := r.first[key]
	var source *resolve.Source
	var err error
	if frozen && first.source != nil && first.source.Commit != "" {
		var found bool
		source, found, err = sourceFromTrialRoot(first, &normalized)
		if err != nil {
			return nil, err
		}
		if !found {
			pinned := normalized
			pinned.Version = first.source.Commit
			if cache, ok := r.wrapped.(interface {
				CachedSource(*resolve.RemoteImport, string) (*resolve.Source, bool, error)
			}); ok {
				source, found, err = cache.CachedSource(&pinned, pinned.Version)
				if err != nil {
					return nil, err
				}
			}
			if !found {
				source, err = r.wrapped.Resolve(&pinned)
				if err != nil {
					return nil, err
				}
			}
		}
	} else {
		source, err = r.wrapped.Resolve(ref)
		if err != nil {
			return nil, err
		}
	}
	r.sources[normalized] = source
	if !frozen {
		r.first[key] = trialSource{ref: normalized, source: source}
	}
	return source, nil
}

func (r *trialResolver) ResolveFrom(
	ref resolve.ImportRef, parent *resolve.Source,
) (*resolve.Source, error) {
	if _, remote := ref.(*resolve.RemoteImport); remote {
		return r.Resolve(ref)
	}
	return resolve.ResolveImportFrom(r.wrapped, ref, parent)
}

func sourceFromTrialRoot(
	first trialSource, ref *resolve.RemoteImport,
) (*resolve.Source, bool, error) {
	projectFS, projectPath := first.source.ProjectFS, first.source.ProjectPath
	if projectFS == nil && first.ref.Subdir == first.ref.ProjectSubdir && first.source.Path != "" {
		projectFS, projectPath = first.source.FS, first.source.Path
	}
	if projectFS == nil {
		return nil, false, nil
	}
	project := ProjectID{URL: ref.URL, Subdir: ref.ProjectSubdir}
	relative, belongs := ProjectContains(project, RemotePackage{URL: ref.URL, Subdir: ref.Subdir})
	if !belongs {
		return nil, false, nil
	}
	packageFS, err := fs.Sub(projectFS, relative)
	if err != nil {
		return nil, false, err
	}
	source := &resolve.Source{
		FS: packageFS, Commit: first.source.Commit,
		ProjectFS: projectFS, ProjectPath: projectPath,
		ProjectSubdir: ref.ProjectSubdir, PackageSubdir: ref.Subdir,
	}
	if projectPath != "" {
		source.Path = filepath.Join(projectPath, filepath.FromSlash(relative))
	}
	marker, err := projectmarker.ClassifyRoot(projectFS)
	if err != nil {
		return nil, false, err
	}
	if marker.Kind == projectmarker.Go {
		source.ModulePath, source.ModuleRootPath = marker.ModulePath, projectPath
		source.GoImportPath = marker.ModulePath
		if relative != "." {
			source.GoImportPath += "/" + relative
		}
	}
	return source, true, nil
}
