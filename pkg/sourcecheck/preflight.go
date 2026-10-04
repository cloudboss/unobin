package sourcecheck

import (
	"cmp"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/resolve"
)

type preflightSource struct {
	ref    *resolve.RemoteImport
	source *resolve.Source
}

type importPreflight struct {
	resolver resolve.Resolver
	versions map[string]string
	sources  map[resolve.RemoteImport]*resolve.Source
	imports  map[string]preflightSource
	packages map[string]golibrary.PackageSource
	contexts map[string]string
	deferred map[string]bool
}

func (p *importPreflight) checkConfigDependency(
	dep resolve.SyntaxDependency, parent *resolve.Source,
) error {
	ref := resolve.ProjectLockVersion(dep.Ref, p.versions)
	remote, isRemote := ref.(*resolve.RemoteImport)
	if isRemote && remote.Version == "" {
		return fmt.Errorf(
			"no version for %s in project-lock.ub; run `unobin deps sync`", dep.Path)
	}
	source, err := resolve.ResolveImportFrom(p, ref, parent)
	if err != nil {
		return err
	}
	if isRemote {
		if err := resolve.CheckRemotePackageBoundary(remote, source); err != nil {
			return err
		}
	}
	classification := resolve.ClassifySource(source)
	if classification.Kind != resolve.SourceGoLibrary {
		return libraryConfigSourceError(dep.Path, classification)
	}
	if isRemote && source.ModulePath != "" {
		if err := resolve.ValidateGoModulePath(remote, source.ModulePath); err != nil {
			return err
		}
	}
	return p.addPackage(preflightSource{ref: remote, source: source}, false,
		fmt.Sprintf("%s %q", dep.Kind, dep.Label))
}

func preflightImports(
	refs map[string]resolve.ImportRef, opts ImportAnalysisOptions, schemas *SchemaCache,
) (*importPreflight, *golibrary.CompatibilityContext, error) {
	context := opts.Compatibility
	if context == nil {
		if schemas.baseError != nil {
			return nil, nil, schemas.baseError
		}
		context = schemas.baseCompatibility
	}
	if err := context.CheckPackages(nil); err != nil {
		return nil, nil, err
	}
	p := &importPreflight{
		resolver: opts.Resolver, versions: opts.Versions,
		sources: map[resolve.RemoteImport]*resolve.Source{},
		imports: map[string]preflightSource{}, packages: map[string]golibrary.PackageSource{},
		contexts: map[string]string{}, deferred: map[string]bool{},
	}
	if _, err := resolve.WalkUBFrom(
		refs, p, p, opts.Versions, importSourceForOptions(opts),
	); err != nil {
		return nil, nil, err
	}
	configDeps, err := bodyLibraryConfigDeps(opts.Body)
	if err != nil {
		return nil, nil, err
	}
	if err := p.checkConfigDependencies(configDeps, importSourceForOptions(opts)); err != nil {
		return nil, nil, err
	}
	packages := make([]golibrary.PackageSource, 0, len(p.packages))
	for _, source := range p.packages {
		packages = append(packages, source)
	}
	slices.SortFunc(packages, func(a, b golibrary.PackageSource) int {
		return cmp.Compare(a.Dir, b.Dir)
	})
	modules := make([]golibrary.ModuleSource, 0, len(packages))
	for _, source := range packages {
		modules = append(modules, source.Module)
	}
	context, err = context.WithModules(modules)
	if err != nil {
		return nil, nil, err
	}
	selection := make(map[deps.Dependency]string, len(opts.Versions))
	for id, version := range opts.Versions {
		dep, err := deps.ParseDependency(id)
		if err != nil {
			return nil, nil, err
		}
		selection[dep] = version
	}
	var moduleResolver resolve.Resolver = p
	if opts.Mode == ModeNoFetch {
		moduleResolver = cachedModuleResolver{wrapped: p}
	}
	context, err = context.WithModuleResolver(func(importPath string) (golibrary.ModuleSource, error) {
		return deps.ResolveSelectedModule(importPath, selection, moduleResolver, nil)
	})
	if err != nil {
		return nil, nil, err
	}
	var failures []error
	for _, source := range packages {
		deferred, err := deferUncachedMetadata(context.CheckPackage(source))
		if deferred {
			p.deferred[source.Dir] = true
		}
		if err != nil {
			failures = append(failures, diagnostic.Context(p.contexts[source.Dir], err))
		}
	}
	if err := errors.Join(failures...); err != nil {
		return nil, nil, err
	}
	return p, context, nil
}

func (p *importPreflight) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	return p.ResolveFrom(ref, nil)
}

func (p *importPreflight) ResolveFrom(
	ref resolve.ImportRef, parent *resolve.Source,
) (*resolve.Source, error) {
	remote, isRemote := ref.(*resolve.RemoteImport)
	if isRemote {
		if source, found := p.sources[*remote]; found {
			if source != nil && p.deferred[source.Path] {
				return opaqueSource(), nil
			}
			return source, nil
		}
	}
	source, err := resolve.ResolveImportFrom(p.resolver, ref, parent)
	if err != nil {
		return nil, err
	}
	if isRemote {
		p.sources[*remote] = source
		importPath := remote.URL
		if remote.Subdir != "" {
			importPath += "/" + remote.Subdir
		}
		if source.GoImportPath != "" {
			importPath = source.GoImportPath
		}
		p.imports[importPath] = preflightSource{ref: remote, source: source}
	}
	if source != nil && p.deferred[source.Path] {
		return opaqueSource(), nil
	}
	return source, nil
}

func (p *importPreflight) OnGoImport(alias, path, _, _ string) error {
	return p.addPackage(p.imports[path], true, fmt.Sprintf("import %q", alias))
}

func (p *importPreflight) OnUBLibrary(
	_, _ string, _ resolve.ImportRef, lib *resolve.UBLibrary,
) error {
	var failures []error
	for _, entry := range lib.CompositeEntries() {
		configDeps, err := bodyLibraryConfigDeps(&entry.SyntaxBody)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if err := p.checkConfigDependencies(configDeps, lib.Source); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (p *importPreflight) checkConfigDependencies(
	deps []resolve.SyntaxDependency, parent *resolve.Source,
) error {
	var failures []error
	for _, dep := range deps {
		if err := p.checkConfigDependency(dep, parent); err != nil {
			failures = append(failures, diagnostic.Context(
				fmt.Sprintf("%s %q", dep.Kind, dep.Label), err))
		}
	}
	return errors.Join(failures...)
}

func (p *importPreflight) addPackage(record preflightSource, linked bool, label string) error {
	if record.source == nil || record.source.Path == "" {
		return nil
	}
	dir, err := filepath.Abs(record.source.Path)
	if err != nil {
		return err
	}
	module, err := golibrary.ModuleSourceAt(dir)
	if err != nil {
		return err
	}
	module.Commit = record.source.Commit
	if ref := record.ref; ref != nil {
		owner := deps.ProjectID{URL: ref.URL, Subdir: ref.ProjectSubdir}
		module.Dependency = owner.String()
		module.Version = p.versions[owner.String()]
		if module.Version == "" {
			module.Version = ref.Version
		}
		if deps.IsReplacementSentinel(module.Version) {
			module.Replacement = module.Dir
		}
	}
	previous := p.packages[dir]
	if p.contexts[dir] == "" || linked && !previous.Linked {
		p.contexts[dir] = label
	}
	p.packages[dir] = golibrary.PackageSource{
		Module: module, Dir: dir, Linked: linked || previous.Linked,
	}
	return nil
}
