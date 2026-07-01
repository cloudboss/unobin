package deps

import (
	"fmt"
	"io/fs"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/resolve"
)

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
	w := &projectLockWalker{
		resolver:    resolver,
		selection:   selection,
		replace:     replace,
		schemaRoots: slices.Clone(schemaRoots),
		projectLock: NewProjectLock(),
		inProgress:  map[string]bool{},
		walked:      map[string]bool{},
	}
	err := fs.WalkDir(rootFS, ".", func(path string, d fs.DirEntry, err error) error {
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
		return w.projectLockFileImports(rootFS, path)
	})
	if err != nil {
		return nil, err
	}
	if err := validateProjectLockDeps(w.projectLock); err != nil {
		return nil, fmt.Errorf("project-lock: %w", err)
	}
	return w.projectLock, nil
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
	for _, ref := range refs {
		if local, ok := ref.Ref.(*resolve.LocalImport); ok {
			if ref.Kind == resolve.SyntaxDependencyLibraryConfig {
				continue
			}
			if err := w.checkLocalImport(rootFS, ref.Label, local, filepath.Dir(path)); err != nil {
				return err
			}
			continue
		}
		r := ref.Ref.(*resolve.RemoteImport)
		if err := w.walkRemote(r, ref.Kind); err != nil {
			return fmt.Errorf("%s %q: %w", ref.Kind, ref.Label, err)
		}
	}
	return nil
}

type projectLockWalker struct {
	resolver    resolve.Resolver
	selection   map[Dependency]string
	replace     map[Dependency]string
	schemaRoots []goschema.ModuleRoot
	projectLock *ProjectLock
	inProgress  map[string]bool
	walked      map[string]bool
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
	for _, ref := range refs {
		var err error
		switch r := ref.Ref.(type) {
		case *resolve.LocalImport:
			if ref.Kind == resolve.SyntaxDependencyLibraryConfig {
				continue
			}
			err = w.walkLocal(r, parent)
		case *resolve.RemoteImport:
			err = w.walkRemote(r, ref.Kind)
		}
		if err != nil {
			return fmt.Errorf("%s %q: %w", ref.Kind, ref.Label, err)
		}
	}
	return nil
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
) error {
	pkg := RemotePackage{URL: r.URL, Subdir: r.Subdir}
	if _, replaced := MostSpecificProject(ProjectIDsFromReplace(w.replace), pkg); replaced {
		return w.walkReplaced(r)
	}
	owner, version, ok := w.ownerVersion(pkg)
	if !ok {
		return fmt.Errorf(
			"%s is imported but has no owning project version in project.ub; "+
				"add one with `unobin deps get <project>@<version>`",
			pkg)
	}
	packageKey := pkg.String() + "@" + version
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
		if err := w.checkRemoteSchemaDependency(pkg, owner, version, src, classification); err != nil {
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
		return missingPackageProjectError(pkg, owner.Project)
	case resolve.SourceUBLibrary:
		kind = ProjectLockKindUB
	case resolve.SourceGoLibrary:
		if src.ModulePath != "" {
			if err := resolve.ValidateGoModulePath(
				remotePackageRef(pkg, owner, version), src.ModulePath,
			); err != nil {
				return err
			}
		}
		if err := validateGoLibrarySource(src); err != nil {
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
) error {
	switch classification.Kind {
	case resolve.SourceFactory:
		return fmt.Errorf("a factory cannot be used as a library-config schema")
	case resolve.SourceInvalid:
		return missingPackageProjectError(pkg, owner.Project)
	case resolve.SourceUBLibrary:
		return fmt.Errorf("library-config schema dependency must resolve to a Go package")
	case resolve.SourceGoLibrary:
		if src.ModulePath != "" {
			if err := resolve.ValidateGoModulePath(
				remotePackageRef(pkg, owner, version), src.ModulePath,
			); err != nil {
				return err
			}
		}
		if err := validateGoLibraryConfigurationSource(src, w.schemaRoots...); err != nil {
			return err
		}
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

func validateGoLibrarySource(src *resolve.Source) error {
	if src == nil || src.Path == "" {
		return nil
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
	extra ...goschema.ModuleRoot,
) error {
	if src == nil || src.Path == "" {
		return nil
	}
	_, _, err := goschema.ReadLibraryConfiguration(src.Path, extra...)
	return err
}

func missingPackageProjectError(pkg RemotePackage, project ProjectID) error {
	return fmt.Errorf(
		"selected project %s does not provide package %s; "+
			"add the owning project to project.ub and run `unobin deps sync`",
		project, pkg)
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

// checkLocalImport resolves a local import and rejects it when it points to a
// Go library, which cannot be imported by path. A UB library is fine: the
// project visit sees its files directly, so nothing more is recorded.
func (w *projectLockWalker) checkLocalImport(
	rootFS fs.FS,
	alias string,
	r *resolve.LocalImport,
	baseDir string,
) error {
	if err := checkLocalImportProjectBoundary(rootFS, baseDir, r.Path); err != nil {
		return fmt.Errorf("import %q: %w", alias, err)
	}
	resolved := &resolve.LocalImport{Path: rebaseLocalPath(baseDir, r.Path)}
	src, err := w.resolver.Resolve(resolved)
	if err != nil {
		return fmt.Errorf("import %q: %w", alias, err)
	}
	classification := resolve.ClassifySource(src)
	switch classification.Kind {
	case resolve.SourceFactory:
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

func (w *projectLockWalker) walkReplaced(r *resolve.RemoteImport) error {
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
	switch resolve.ClassifySource(src).Kind {
	case resolve.SourceFactory:
		return fmt.Errorf("a factory cannot be imported")
	case resolve.SourceUBLibrary:
		return w.walkBodies(src)
	case resolve.SourceGoLibrary:
		return validateGoLibrarySource(src)
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
	for _, name := range matches {
		b, err := fs.ReadFile(src.FS, name)
		if err != nil {
			return err
		}
		if err := w.walkBodyFile(name, b, src); err != nil {
			return err
		}
	}
	return nil
}
