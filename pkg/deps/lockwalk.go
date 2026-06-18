package deps

import (
	"fmt"
	"io/fs"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/resolve"
)

// LockFromImports builds the lock for the project rooted at rootFS. It
// walks every .ub file under the root -- factory.ub, library files at the
// root, or libraries in subdirectories --
// and through remote UB libraries their imports too. Each remote library
// becomes one lock entry, keyed by `repo//subdir`. Local imports are not
// locked and need no following: the walk already visits every file under the
// root, so a local library's own imports are reached directly. A library's
// version is its repository's selected version; a repository the selection
// does not cover is an error (it is imported but no floor reached it). Kind
// and content hash come from the fetched library subtree, so a Go library and
// a UB library in the same repo are recorded distinctly. A repository named
// in replace is read from its local path and never locked; a replaced UB
// library's own remote dependencies are still walked.
func LockFromImports(
	rootFS fs.FS, selection map[Dependency]string, resolver resolve.Resolver,
	replace map[Dependency]string,
) (*Lock, error) {
	w := &lockWalker{
		resolver:   resolver,
		selection:  selection,
		replace:    replace,
		lock:       NewLock(),
		inProgress: map[string]bool{},
		walked:     map[string]bool{},
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
			hasManifest, err := fsHasManifestFile(rootFS, path)
			if err != nil {
				return err
			}
			if hasManifest {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".ub") {
			return nil
		}
		return w.lockFileImports(rootFS, path)
	})
	if err != nil {
		return nil, err
	}
	if err := validateLockedDeps(w.lock); err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	return w.lock, nil
}

// lockFileImports reads one of the project's own .ub files, locks the
// remote libraries it imports, and checks each local import. A local
// import's UB library is not locked -- the project walk visits its files
// on their own -- but a local import that points to a Go library is
// rejected, the same as at compile.
func (w *lockWalker) lockFileImports(rootFS fs.FS, path string) error {
	b, err := fs.ReadFile(rootFS, path)
	if err != nil {
		return err
	}
	refs, err := lockFileImportRefs(path, b)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if local, ok := ref.Ref.(*resolve.LocalImport); ok {
			if err := w.checkLocalImport(rootFS, ref.Label, local, filepath.Dir(path)); err != nil {
				return err
			}
			continue
		}
		r := ref.Ref.(*resolve.RemoteImport)
		if err := w.walkRemote(r); err != nil {
			return fmt.Errorf("import %q: %w", ref.Label, err)
		}
	}
	return nil
}

type lockWalker struct {
	resolver   resolve.Resolver
	selection  map[Dependency]string
	replace    map[Dependency]string
	lock       *Lock
	inProgress map[string]bool
	walked     map[string]bool
}

type lockImportRef struct {
	Label string
	Ref   resolve.ImportRef
}

func lockFileImportRefs(path string, src []byte) ([]lockImportRef, error) {
	refs, err := extractSyntaxImportRefs(path, src)
	if err != nil {
		return nil, err
	}
	out := make([]lockImportRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, lockImportRef{
			Label: syntaxImportLabel(ref),
			Ref:   ref.Ref,
		})
	}
	slices.SortFunc(out, func(a, b lockImportRef) int {
		return strings.Compare(a.Label, b.Label)
	})
	return out, nil
}

func syntaxImportLabel(ref resolve.SyntaxImport) string {
	if ref.Scope == "" {
		return ref.Alias
	}
	return ref.Scope + "." + ref.Alias
}

func (w *lockWalker) walkBodyFile(path string, src []byte, parent *resolve.Source) error {
	refs, err := lockFileImportRefs(path, src)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		var err error
		switch r := ref.Ref.(type) {
		case *resolve.LocalImport:
			err = w.walkLocal(r, parent)
		case *resolve.RemoteImport:
			err = w.walkRemote(r)
		}
		if err != nil {
			return fmt.Errorf("import %q: %w", ref.Label, err)
		}
	}
	return nil
}

func (w *lockWalker) walkLocal(r *resolve.LocalImport, parent *resolve.Source) error {
	src, err := resolve.ResolveLocalSource(r, parent)
	if err != nil {
		return err
	}
	return w.walkBodies(src)
}

func (w *lockWalker) walkRemote(r *resolve.RemoteImport) error {
	pkg := RemotePackage{URL: r.URL, Subdir: r.Subdir}
	if _, replaced := MostSpecificProject(ProjectIDsFromReplace(w.replace), pkg); replaced {
		return w.walkReplaced(r)
	}
	owner, version, ok := w.ownerVersion(pkg)
	if !ok {
		return fmt.Errorf(
			"%s is imported but has no owning project version in the dependency manifest; "+
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
	if resolve.ContainsFactorySource(src) {
		return fmt.Errorf("a factory cannot be imported")
	}
	kind := LockKindGo
	if resolve.IsUBLibrary(src) {
		kind = LockKindUB
	}
	projectID := owner.Project.String()
	if _, done := w.lock.Deps[projectID]; !done {
		entry, err := w.lockEntry(owner.Project, version, kind, src)
		if err != nil {
			return err
		}
		w.lock.Deps[projectID] = entry
	}
	if kind == LockKindUB {
		if err := w.walkBodies(src); err != nil {
			return err
		}
	}
	w.walked[packageKey] = true
	return nil
}

func (w *lockWalker) ownerVersion(pkg RemotePackage) (PackageOwner, string, bool) {
	owner, ok := MostSpecificProject(ProjectIDsFromDependencies(w.selection), pkg)
	if !ok {
		return PackageOwner{}, "", false
	}
	version, ok := w.selection[owner.Project.Dependency()]
	return owner, version, ok
}

func (w *lockWalker) lockEntry(
	project ProjectID, version string, kind LockKind, src *resolve.Source,
) (*LockedDep, error) {
	entry := &LockedDep{Kind: kind, Version: version, Commit: src.Commit}
	if kind != LockKindUB {
		return entry, nil
	}
	projectSrc, err := w.resolver.Resolve(remoteProjectRef(project, version))
	if err != nil {
		return nil, err
	}
	entry.Commit = projectSrc.Commit
	entry.Hash = projectSrc.Hash
	if entry.Hash == "" {
		var err error
		entry.Hash, err = resolve.HashTree(projectSrc.FS)
		if err != nil {
			return nil, err
		}
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

// checkLocalImport resolves a local import and rejects it when it points
// to a Go library, which cannot be imported by path. A UB library is fine:
// the project walk visits its files directly, so nothing more is recorded.
func (w *lockWalker) checkLocalImport(
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
	hasFactory := resolve.ContainsFactorySource(src)
	hasExports := resolve.HasCompositeExports(src)
	if hasFactory && !hasExports {
		return fmt.Errorf("import %q: %s is not a UB library", alias, r.Path)
	}
	if !hasExports {
		return resolve.LocalGoImportError(alias, r.Path, src)
	}
	return nil
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
	importerProject, importerOK, err := nearestManifestInFS(rootFS, cleanFSPath(baseDir))
	if err != nil {
		return err
	}
	targetProject, targetOK, err := nearestManifestInFS(rootFS, target)
	if err != nil {
		return err
	}
	if importerOK && targetOK && importerProject != targetProject {
		return localImportProjectBoundaryError(pathpkg.Clean(importPath))
	}
	return nil
}

// walkReplaced handles an import whose repository the manifest replaces
// with a local path. The resolver serves it from disk; like a local
// import it is not locked (its content is whatever is on disk), but a UB
// library's own remote dependencies are still walked and locked.
func (w *lockWalker) walkReplaced(r *resolve.RemoteImport) error {
	src, err := w.resolver.Resolve(&resolve.RemoteImport{URL: r.URL, Subdir: r.Subdir})
	if err != nil {
		return err
	}
	if resolve.ContainsFactorySource(src) {
		return fmt.Errorf("a factory cannot be imported")
	}
	if resolve.IsUBLibrary(src) {
		return w.walkBodies(src)
	}
	return nil
}

func (w *lockWalker) walkBodies(src *resolve.Source) error {
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
