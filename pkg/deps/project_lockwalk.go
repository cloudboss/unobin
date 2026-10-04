package deps

import (
	"errors"
	"fmt"
	"io/fs"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/resolve"
)

type ProjectLockOptions struct {
	SchemaRoots   []goschema.ModuleRoot
	Compatibility *golibrary.CompatibilityContext
}

type ProjectLockPreparation struct {
	Lock          *ProjectLock
	Compatibility *golibrary.CompatibilityContext
}

type selectedGoPackage struct {
	source        *resolve.Source
	metadata      golibrary.PackageSource
	library       bool
	configuration bool
	context       string
}

func (w *projectLockWalker) collectGoPackage(
	source *resolve.Source, owner PackageOwner, version string,
	kind resolve.SyntaxDependencyKind, label string,
) error {
	if source == nil || source.Path == "" {
		return nil
	}
	files, err := fs.ReadDir(source.FS, ".")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	hasGoPackage := false
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".go") &&
			!strings.HasSuffix(file.Name(), "_test.go") {
			hasGoPackage = true
			break
		}
	}
	if !hasGoPackage {
		pkg := RemotePackage{URL: owner.Project.URL, Subdir: owner.Project.Subdir}
		if owner.PackageSubdir != "." {
			pkg.Subdir = pathpkg.Join(pkg.Subdir, owner.PackageSubdir)
		}
		return missingPackageProjectError(pkg, owner.Project, version, source)
	}
	dir, err := filepath.Abs(source.Path)
	if err != nil {
		return err
	}
	module, err := golibrary.ModuleSourceAt(dir)
	if err != nil {
		return err
	}
	module.Dependency = owner.Project.String()
	module.Version, module.Commit = version, source.Commit
	if _, replaced := w.replace[owner.Project.Dependency()]; replaced {
		module.Replacement = module.Dir
		module.Version, module.Commit = "", ""
	}
	if version != "" && module.Replacement == "" {
		ref := &resolve.RemoteImport{
			URL: owner.Project.URL, Subdir: owner.Project.Subdir,
			ProjectSubdir: owner.Project.Subdir, Version: ProjectTag(owner.Project, version),
		}
		if err := resolve.ValidateGoModulePath(ref, module.Path); err != nil {
			var mismatch *resolve.ModulePathError
			if errors.As(err, &mismatch) {
				mismatch.Commit = source.Commit
			}
			return err
		}
	}
	entry := w.packages[dir]
	if entry == nil {
		entry = &selectedGoPackage{source: source, context: label}
		w.packages[dir] = entry
	}
	if kind == resolve.SyntaxDependencyImport && !entry.library {
		entry.context = label
	}
	entry.library = entry.library || kind == resolve.SyntaxDependencyImport
	entry.configuration = entry.configuration || kind == resolve.SyntaxDependencyLibraryConfig
	entry.metadata = golibrary.PackageSource{Module: module, Dir: dir, Linked: entry.library}
	return nil
}

func (w *projectLockWalker) validateSelectedPackages() error {
	dirs := make([]string, 0, len(w.packages))
	for dir := range w.packages {
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	modules := make([]golibrary.ModuleSource, 0, len(dirs))
	for _, dir := range dirs {
		modules = append(modules, w.packages[dir].metadata.Module)
	}
	context, err := w.compatibility.WithModules(modules)
	if err != nil {
		return err
	}
	context, err = context.WithModuleResolver(func(importPath string) (golibrary.ModuleSource, error) {
		return ResolveSelectedModule(importPath, w.selection, w.resolver, w.replace)
	})
	if err != nil {
		return err
	}
	w.compatibility = context
	failures := slices.Clone(w.discoveryErrors)
	for _, dir := range dirs {
		entry := w.packages[dir]
		if err := context.CheckPackage(entry.metadata); err != nil {
			failures = append(failures, diagnostic.Context(entry.context, err))
		}
	}
	if err := errors.Join(failures...); err != nil {
		return err
	}
	for _, metadata := range context.Manifest() {
		module := metadata.Source.Module
		if module.Dependency == "" || module.Replacement != "" {
			continue
		}
		if _, found := w.projectLock.Deps[module.Dependency]; !found {
			w.projectLock.Deps[module.Dependency] = &ProjectLockDep{
				Kind: ProjectLockKindGo, Version: module.Version, Commit: module.Commit,
			}
		}
	}
	w.schemaRoots = nil
	for _, module := range context.ModuleSources() {
		w.schemaRoots = append(w.schemaRoots, goschema.ModuleRoot{Path: module.Path, Dir: module.Dir})
	}
	for _, dir := range dirs {
		entry := w.packages[dir]
		if entry.library {
			if err := validateGoLibrarySource(entry.source, context); err != nil {
				failures = append(failures, diagnostic.Context(entry.context, err))
			}
		}
	}
	for _, metadata := range context.Manifest() {
		entry := w.packages[metadata.Source.Dir]
		if !metadata.HasConfigurationEntryPoint && (entry == nil || !entry.configuration) {
			continue
		}
		label := fmt.Sprintf("configuration package %q", metadata.Package)
		if entry != nil {
			label = entry.context
		}
		if err := validateGoLibraryConfigurationSource(
			&resolve.Source{Path: metadata.Source.Dir}, context, w.schemaRoots...,
		); err != nil {
			failures = append(failures, diagnostic.Context(label, err))
		}
	}
	return errors.Join(failures...)
}

// ProjectLockFromImports builds the project-lock for the project rooted at
// rootFS. It visits every .ub file under the root -- factory.ub, library files
// at the root, or libraries in subdirectories -- and visits remote UB library
// imports too. Each remote library becomes one selected dependency entry,
// keyed by `repo//subdir`. Local imports are not selected separately because
// the project visit already sees every file under the root. A library's version
// is its repository's selected version; a repository the selection does not
// cover is an error. Go dependencies are recorded by project id. UB
// dependencies are recorded by project id with a project-root hash. A repository
// named in replace is read from its local path and never added to the
// project-lock; a replaced UB library's own remote dependencies are still
// visited.
func ProjectLockFromImports(
	rootFS fs.FS,
	selection map[Dependency]string,
	resolver resolve.Resolver,
	replace map[Dependency]string,
) (*ProjectLock, error) {
	return ProjectLockFromImportsWithSchemaRoots(rootFS, selection, resolver, replace, nil)
}

// ProjectLockFromImportsWithSchemaRoots is ProjectLockFromImports with extra
// Go module roots available to config schema validation.
func ProjectLockFromImportsWithSchemaRoots(
	rootFS fs.FS,
	selection map[Dependency]string,
	resolver resolve.Resolver,
	replace map[Dependency]string,
	schemaRoots []goschema.ModuleRoot,
) (*ProjectLock, error) {
	prepared, err := PrepareProjectLock(rootFS, selection, resolver, replace,
		ProjectLockOptions{SchemaRoots: schemaRoots})
	if err != nil {
		return nil, err
	}
	return prepared.Lock, nil
}

func PrepareProjectLock(
	rootFS fs.FS,
	selection map[Dependency]string,
	resolver resolve.Resolver,
	replace map[Dependency]string,
	opts ProjectLockOptions,
) (*ProjectLockPreparation, error) {
	modules := make([]golibrary.ModuleSource, 0, len(opts.SchemaRoots))
	for _, root := range opts.SchemaRoots {
		modules = append(modules, golibrary.ModuleSource{Path: root.Path, Dir: root.Dir})
	}
	context := opts.Compatibility
	var err error
	if context == nil {
		context, err = golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{Modules: modules})
	} else {
		context, err = context.WithModules(modules)
	}
	if err != nil {
		return nil, err
	}
	w := &projectLockWalker{
		resolver:      resolver,
		selection:     selection,
		replace:       replace,
		schemaRoots:   slices.Clone(opts.SchemaRoots),
		compatibility: context,
		packages:      map[string]*selectedGoPackage{},
		projectLock:   NewProjectLock(),
		inProgress:    map[string]bool{},
		walked:        map[string]bool{},
	}
	err = fs.WalkDir(rootFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == "." {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			hasProject, err := fsHasProjectFile(rootFS, path)
			if err != nil {
				return err
			}
			if hasProject {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".ub") {
			return nil
		}
		if err := w.projectLockFileImports(rootFS, path); err != nil {
			w.discoveryErrors = append(w.discoveryErrors, err)
		}
		return nil
	})
	if err != nil {
		w.discoveryErrors = append(w.discoveryErrors, err)
	}
	if err := w.validateSelectedPackages(); err != nil {
		return nil, err
	}
	if err := validateProjectLockDeps(w.projectLock); err != nil {
		return nil, fmt.Errorf("project-lock: %w", err)
	}
	return &ProjectLockPreparation{Lock: w.projectLock, Compatibility: w.compatibility}, nil
}

func (w *projectLockWalker) projectLockFileImports(rootFS fs.FS, path string) error {
	b, err := fs.ReadFile(rootFS, path)
	if err != nil {
		return err
	}
	refs, err := projectLockFileImportRefs(path, b)
	if err != nil {
		return err
	}
	var failures []error
	for _, ref := range refs {
		if local, ok := ref.Ref.(*resolve.LocalImport); ok {
			if ref.Kind == resolve.SyntaxDependencyLibraryConfig {
				source, err := w.resolver.Resolve(&resolve.LocalImport{
					Path: rebaseLocalPath(filepath.Dir(path), local.Path),
				})
				if err != nil {
					failures = append(failures, diagnostic.Context(ref.Label, err))
					continue
				}
				label := fmt.Sprintf("%s %q", ref.Kind, ref.Label)
				if err := w.validateSchemaDependencySource(RemotePackage{}, PackageOwner{}, "",
					source, resolve.ClassifySource(source), label); err != nil {
					failures = append(failures, diagnostic.Context(label, err))
				}
				continue
			}
			if err := w.checkLocalImport(rootFS, ref.Label, local, filepath.Dir(path)); err != nil {
				failures = append(failures, err)
			}
			continue
		}
		r := ref.Ref.(*resolve.RemoteImport)
		if err := w.walkRemote(r, ref.Kind, fmt.Sprintf("%s %q", ref.Kind, ref.Label)); err != nil {
			failures = append(failures, fmt.Errorf("%s %q: %w", ref.Kind, ref.Label, err))
			continue
		}
	}
	return errors.Join(failures...)
}

type projectLockWalker struct {
	resolver        resolve.Resolver
	selection       map[Dependency]string
	replace         map[Dependency]string
	schemaRoots     []goschema.ModuleRoot
	projectLock     *ProjectLock
	inProgress      map[string]bool
	walked          map[string]bool
	compatibility   *golibrary.CompatibilityContext
	packages        map[string]*selectedGoPackage
	discoveryErrors []error
}

type projectLockImportRef struct {
	Label string
	Kind  resolve.SyntaxDependencyKind
	Ref   resolve.ImportRef
}

func projectLockFileImportRefs(path string, src []byte) ([]projectLockImportRef, error) {
	refs, err := extractSyntaxDependencyRefs(path, src)
	if err != nil {
		return nil, err
	}
	out := make([]projectLockImportRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, projectLockImportRef{
			Label: ref.Label,
			Kind:  ref.Kind,
			Ref:   ref.Ref,
		})
	}
	slices.SortFunc(out, func(a, b projectLockImportRef) int {
		return strings.Compare(a.Label, b.Label)
	})
	return out, nil
}

func (w *projectLockWalker) walkBodyFile(path string, src []byte, parent *resolve.Source) error {
	refs, err := projectLockFileImportRefs(path, src)
	if err != nil {
		return err
	}
	var failures []error
	for _, ref := range refs {
		var err error
		switch r := ref.Ref.(type) {
		case *resolve.LocalImport:
			if ref.Kind == resolve.SyntaxDependencyLibraryConfig {
				source, resolveErr := resolve.ResolveLocalSource(r, parent)
				if resolveErr != nil {
					failures = append(failures, diagnostic.Context(ref.Label, resolveErr))
					continue
				}
				label := fmt.Sprintf("%s %q", ref.Kind, ref.Label)
				if err := w.validateSchemaDependencySource(RemotePackage{}, PackageOwner{}, "",
					source, resolve.ClassifySource(source), label); err != nil {
					failures = append(failures, diagnostic.Context(label, err))
				}
				continue
			}
			err = w.walkLocal(r, parent)
		case *resolve.RemoteImport:
			err = w.walkRemote(r, ref.Kind, fmt.Sprintf("%s %q", ref.Kind, ref.Label))
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s %q: %w", ref.Kind, ref.Label, err))
		}
	}
	return errors.Join(failures...)
}

func (w *projectLockWalker) walkLocal(r *resolve.LocalImport, parent *resolve.Source) error {
	src, err := resolve.ResolveLocalSource(r, parent)
	if err != nil {
		return err
	}
	return w.walkBodies(src)
}

func (w *projectLockWalker) walkRemote(
	r *resolve.RemoteImport,
	depKind resolve.SyntaxDependencyKind,
	label string,
) error {
	pkg := RemotePackage{URL: r.URL, Subdir: r.Subdir}
	if _, replaced := MostSpecificProject(ProjectIDsFromReplace(w.replace), pkg); replaced {
		return w.walkReplaced(r, depKind, label)
	}
	owner, version, ok := w.ownerVersion(pkg)
	if !ok {
		return &SourceSelectionError{Package: pkg.String(), Message: fmt.Sprintf(
			"%s is imported but has no owning project version in project.ub; "+
				"add one with `unobin deps get <project>@<version>`",
			pkg)}
	}
	packageKey := pkg.String() + "@" + version + "::" + string(depKind)
	if w.walked[packageKey] {
		return nil
	}
	if w.inProgress[packageKey] {
		return fmt.Errorf("import cycle through %s", packageKey)
	}
	w.inProgress[packageKey] = true
	defer delete(w.inProgress, packageKey)

	src, err := w.resolver.Resolve(remotePackageRef(pkg, owner, version))
	if err != nil {
		return err
	}
	if err := CheckPackageBoundary(src, owner, pkg); err != nil {
		return err
	}
	classification := resolve.ClassifySource(src)
	if depKind == resolve.SyntaxDependencyLibraryConfig {
		if err := w.checkRemoteSchemaDependency(
			pkg, owner, version, src, classification, label,
		); err != nil {
			return err
		}
		w.walked[packageKey] = true
		return nil
	}
	kind := ProjectLockKindGo
	switch classification.Kind {
	case resolve.SourceFactory:
		return fmt.Errorf("a factory cannot be imported")
	case resolve.SourceInvalid:
		return missingPackageProjectError(pkg, owner.Project, version, src)
	case resolve.SourceUBLibrary:
		kind = ProjectLockKindUB
	case resolve.SourceGoLibrary:
		if src.ModulePath != "" {
			if err := resolve.ValidateGoModulePath(
				remotePackageRef(pkg, owner, version), src.ModulePath,
			); err != nil {
				var mismatch *resolve.ModulePathError
				if errors.As(err, &mismatch) {
					mismatch.Commit = src.Commit
				}
				return err
			}
		}
		if err := w.collectGoPackage(src, owner, version, depKind, label); err != nil {
			return err
		}
	}
	projectID := owner.Project.String()
	if _, done := w.projectLock.Deps[projectID]; !done {
		entry, err := w.projectLockEntry(owner.Project, version, kind, src)
		if err != nil {
			return err
		}
		w.projectLock.Deps[projectID] = entry
	}
	if kind == ProjectLockKindUB {
		if err := w.walkBodies(src); err != nil {
			return err
		}
	}
	w.walked[packageKey] = true
	return nil
}

func (w *projectLockWalker) checkRemoteSchemaDependency(
	pkg RemotePackage,
	owner PackageOwner,
	version string,
	src *resolve.Source,
	classification resolve.SourceClassification,
	label string,
) error {
	if err := w.validateSchemaDependencySource(
		pkg, owner, version, src, classification, label,
	); err != nil {
		return err
	}
	projectID := owner.Project.String()
	if _, done := w.projectLock.Deps[projectID]; done {
		return nil
	}
	entry, err := w.projectLockEntry(owner.Project, version, ProjectLockKindGo, src)
	if err != nil {
		return err
	}
	w.projectLock.Deps[projectID] = entry
	return nil
}

func (w *projectLockWalker) validateSchemaDependencySource(
	pkg RemotePackage,
	owner PackageOwner,
	version string,
	src *resolve.Source,
	classification resolve.SourceClassification,
	label string,
) error {
	switch classification.Kind {
	case resolve.SourceFactory:
		return fmt.Errorf("a factory cannot be used as a library-config schema")
	case resolve.SourceInvalid:
		return missingPackageProjectError(pkg, owner.Project, version, src)
	case resolve.SourceUBLibrary:
		return fmt.Errorf("library-config schema dependency must resolve to a Go package")
	case resolve.SourceGoLibrary:
		if version != "" && src.ModulePath != "" {
			if err := resolve.ValidateGoModulePath(
				remotePackageRef(pkg, owner, version), src.ModulePath,
			); err != nil {
				var mismatch *resolve.ModulePathError
				if errors.As(err, &mismatch) {
					mismatch.Commit = src.Commit
				}
				return err
			}
		}
		return w.collectGoPackage(src, owner, version, resolve.SyntaxDependencyLibraryConfig, label)
	}
	return nil
}

func validateGoLibrarySource(src *resolve.Source, context *golibrary.CompatibilityContext) error {
	if src == nil || src.Path == "" {
		return nil
	}
	if err := context.CheckDirectory(src.Path, true); err != nil {
		return err
	}
	moduleRoot, err := golibrary.FindModuleRoot(src.Path)
	if err != nil {
		return err
	}
	_, err = golibrary.ValidatePackage(moduleRoot, src.Path)
	return err
}

func validateGoLibraryConfigurationSource(
	src *resolve.Source,
	context *golibrary.CompatibilityContext,
	extra ...goschema.ModuleRoot,
) error {
	if src == nil || src.Path == "" {
		return nil
	}
	if err := context.CheckDirectory(src.Path, false); err != nil {
		return err
	}
	_, _, err := goschema.ReadLibraryConfiguration(src.Path, extra...)
	return err
}

func missingPackageProjectError(
	pkg RemotePackage, project ProjectID, version string, src *resolve.Source,
) error {
	if _, err := fs.ReadDir(src.FS, "."); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return &SourceSelectionError{
		Dependency: project.String(), Package: pkg.String(), Version: version, Commit: src.Commit,
		Message: fmt.Sprintf(
			"selected project %s does not provide package %s; "+
				"add the owning project to project.ub and run `unobin deps sync`",
			project, pkg),
	}
}

func (w *projectLockWalker) ownerVersion(pkg RemotePackage) (PackageOwner, string, bool) {
	owner, ok := MostSpecificProject(ProjectIDsFromDependencies(w.selection), pkg)
	if !ok {
		return PackageOwner{}, "", false
	}
	version, ok := w.selection[owner.Project.Dependency()]
	return owner, version, ok
}

func (w *projectLockWalker) projectLockEntry(
	project ProjectID,
	version string,
	kind ProjectLockKind,
	src *resolve.Source,
) (*ProjectLockDep, error) {
	entry := &ProjectLockDep{Kind: kind, Version: version, Commit: src.Commit}
	if kind != ProjectLockKindUB {
		return entry, nil
	}
	projectSrc, err := w.resolver.Resolve(remoteProjectRef(project, version))
	if err != nil {
		return nil, err
	}
	entry.Commit = projectSrc.Commit
	entry.Hash, err = HashUBProject(projectSrc.FS)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

func remotePackageRef(pkg RemotePackage, owner PackageOwner, version string) *resolve.RemoteImport {
	return &resolve.RemoteImport{
		URL:           pkg.URL,
		Subdir:        pkg.Subdir,
		ProjectSubdir: owner.Project.Subdir,
		PackageSubdir: pkg.Subdir,
		Version:       ProjectTag(owner.Project, version),
	}
}

func remoteProjectRef(project ProjectID, version string) *resolve.RemoteImport {
	return &resolve.RemoteImport{
		URL:           project.URL,
		Subdir:        project.Subdir,
		ProjectSubdir: project.Subdir,
		PackageSubdir: project.Subdir,
		Version:       ProjectTag(project, version),
	}
}

// checkLocalImport resolves a local import and rejects paths that compile would
// reject. A UB library is fine: the project visit sees its files directly, so
// nothing more is recorded.
func (w *projectLockWalker) checkLocalImport(
	rootFS fs.FS,
	alias string,
	r *resolve.LocalImport,
	baseDir string,
) error {
	if err := checkLocalImportProjectBoundary(rootFS, baseDir, r.Path); err != nil {
		return fmt.Errorf("import %q: %w", alias, err)
	}
	resolvedPath := rebaseLocalPath(baseDir, r.Path)
	resolved := &resolve.LocalImport{Path: resolvedPath}
	src, err := w.resolver.Resolve(resolved)
	if err != nil {
		return fmt.Errorf("import %q: %w", alias, err)
	}
	classification := resolve.ClassifySource(src)
	switch classification.Kind {
	case resolve.SourceFactory:
		if cleanFSPath(resolvedPath) != cleanFSPath(baseDir) {
			return fmt.Errorf("import %q: a factory cannot be imported", alias)
		}
		if !classification.HasCompositeExports {
			return fmt.Errorf("import %q: %s is not a UB library", alias, r.Path)
		}
		return nil
	case resolve.SourceUBLibrary:
		return nil
	default:
		return resolve.LocalGoImportError(alias, r.Path, src)
	}
}

func rebaseLocalPath(baseDir, importPath string) string {
	if filepath.IsAbs(importPath) || baseDir == "." || baseDir == "" {
		return importPath
	}
	return filepath.ToSlash(filepath.Clean(filepath.Join(baseDir, importPath)))
}

func checkLocalImportProjectBoundary(rootFS fs.FS, baseDir, importPath string) error {
	if filepath.IsAbs(importPath) {
		return nil
	}
	target := cleanFSPath(rebaseLocalPath(baseDir, importPath))
	if target == ".." || strings.HasPrefix(target, "../") {
		return nil
	}
	importerProject, importerOK, err := nearestProjectInFS(rootFS, cleanFSPath(baseDir))
	if err != nil {
		return err
	}
	targetProject, targetOK, err := nearestProjectInFS(rootFS, target)
	if err != nil {
		return err
	}
	if importerOK && targetOK && importerProject != targetProject {
		return localImportProjectBoundaryError(pathpkg.Clean(importPath))
	}
	return nil
}

func (w *projectLockWalker) walkReplaced(
	r *resolve.RemoteImport,
	depKind resolve.SyntaxDependencyKind,
	label string,
) error {
	pkg := RemotePackage{URL: r.URL, Subdir: r.Subdir}
	owner, ok := MostSpecificProject(ProjectIDsFromReplace(w.replace), pkg)
	if !ok {
		return fmt.Errorf("%s has no replacement", pkg)
	}
	src, err := w.resolver.Resolve(&resolve.RemoteImport{URL: r.URL, Subdir: r.Subdir})
	if err != nil {
		return err
	}
	if err := CheckPackageBoundary(src, owner, pkg); err != nil {
		return err
	}
	classification := resolve.ClassifySource(src)
	if depKind == resolve.SyntaxDependencyLibraryConfig {
		return w.validateSchemaDependencySource(pkg, owner, "", src, classification, label)
	}
	switch classification.Kind {
	case resolve.SourceFactory:
		return fmt.Errorf("a factory cannot be imported")
	case resolve.SourceUBLibrary:
		return w.walkBodies(src)
	case resolve.SourceGoLibrary:
		return w.collectGoPackage(src, owner, "", resolve.SyntaxDependencyImport, label)
	default:
		return nil
	}
}

func (w *projectLockWalker) walkBodies(src *resolve.Source) error {
	matches, err := fs.Glob(src.FS, "*.ub")
	if err != nil {
		return err
	}
	slices.Sort(matches)
	var failures []error
	for _, name := range matches {
		b, err := fs.ReadFile(src.FS, name)
		if err != nil {
			return err
		}
		if err := w.walkBodyFile(name, b, src); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
