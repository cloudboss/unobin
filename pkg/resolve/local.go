package resolve

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/projectmarker"
)

// LocalResolver resolves *LocalImport refs against a working directory
// root. Relative paths in the import are joined to Root.
type LocalResolver struct {
	Root string
}

// NewLocalResolver returns a LocalResolver rooted at root. Pass the
// directory containing the factory or library files that own the imports.
func NewLocalResolver(root string) *LocalResolver {
	return &LocalResolver{Root: root}
}

// Resolve implements Resolver. The ref must be a *LocalImport; remote
// refs return an error so a misrouted call is reported clearly.
func (r *LocalResolver) Resolve(ref ImportRef) (*Source, error) {
	li, ok := ref.(*LocalImport)
	if !ok {
		return nil, fmt.Errorf("local resolver cannot handle %T", ref)
	}
	if filepath.IsAbs(li.Path) {
		return nil, absoluteLocalImportError(li.Path)
	}
	abs := filepath.Join(r.Root, li.Path)
	if err := checkLocalPathSymlinks(r.Root, li.Path); err != nil {
		return nil, err
	}
	if err := checkLocalProjectBoundary(r.Root, abs, li.Path); err != nil {
		return nil, err
	}
	source, err := localSourceFromPath(li.Path, abs)
	if err != nil {
		return nil, err
	}
	source.LocalPath = filepath.Clean(abs)
	if err := addLocalProjectMetadata(source, r.Root); err != nil {
		return nil, err
	}
	return source, nil
}

// ResolveFrom resolves local refs relative to the package that declared
// them. Remote refs still return an error because LocalResolver handles only
// local filesystem paths.
func (r *LocalResolver) ResolveFrom(ref ImportRef, parent *Source) (*Source, error) {
	li, ok := ref.(*LocalImport)
	if !ok {
		return nil, fmt.Errorf("local resolver cannot handle %T", ref)
	}
	if parent == nil {
		return r.Resolve(ref)
	}
	return ResolveLocalSource(li, parent)
}

// ResolveLocalSource resolves a local import from the package source that
// declared it. On-disk sources resolve through their Path; virtual sources
// resolve paths that stay within their fs.FS root.
func ResolveLocalSource(li *LocalImport, parent *Source) (*Source, error) {
	if parent == nil {
		return nil, fmt.Errorf("local import %q: missing declaring source", li.Path)
	}
	if parent.Path != "" {
		if filepath.IsAbs(li.Path) {
			return nil, absoluteLocalImportError(li.Path)
		}
		abs := filepath.Join(parent.Path, li.Path)
		if err := checkLocalPathSymlinks(parent.Path, li.Path); err != nil {
			return nil, err
		}
		if err := checkLocalProjectBoundary(parent.Path, abs, li.Path); err != nil {
			return nil, err
		}
		child, err := localSourceFromPath(li.Path, abs)
		if err != nil {
			return nil, err
		}
		child.LocalPath = childLocalPath(parent, li.Path)
		preserveLocalProjectMetadata(child, parent, li.Path)
		return child, nil
	}
	if parent.FS == nil {
		return nil, fmt.Errorf("local import %q: missing declaring source", li.Path)
	}
	if filepath.IsAbs(li.Path) {
		return nil, fmt.Errorf("local import %q: absolute path has no filesystem root", li.Path)
	}
	clean := pathpkg.Clean(filepath.ToSlash(li.Path))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, fmt.Errorf("local import %q: cannot resolve above source root", li.Path)
	}
	info, err := fs.Stat(parent.FS, clean)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local import %q: not a directory", li.Path)
	}
	sub, err := fs.Sub(parent.FS, clean)
	if err != nil {
		return nil, err
	}
	child := &Source{FS: sub, LocalPath: childLocalPath(parent, li.Path)}
	preserveLocalProjectMetadata(child, parent, li.Path)
	return child, nil
}

func childLocalPath(parent *Source, importPath string) string {
	base := parent.LocalPath
	if base == "" {
		base = parent.Path
	}
	if base == "" {
		return filepath.Clean(importPath)
	}
	return filepath.Clean(filepath.Join(base, importPath))
}

func absoluteLocalImportError(path string) error {
	return fmt.Errorf("local import %q: absolute paths are not supported", path)
}

func checkLocalPathSymlinks(importerDir, importPath string) error {
	cur := importerDir
	for part := range strings.SplitSeq(filepath.Clean(importPath), string(filepath.Separator)) {
		switch part {
		case "", ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("local import %q: symlink %s is not supported", importPath, cur)
		}
	}
	return nil
}

func checkLocalProjectBoundary(importerDir, targetDir, importPath string) error {
	importerProject, importerOK, err := nearestProjectDir(importerDir)
	if err != nil {
		return err
	}
	if !importerOK {
		return nil
	}
	inside, err := pathWithinDir(importerProject, targetDir)
	if err != nil {
		return err
	}
	if !inside {
		return fmt.Errorf("local import %q resolves outside project root %s",
			importPath, importerProject)
	}
	targetProject, targetOK, err := nearestProjectDir(targetDir)
	if err != nil {
		return err
	}
	if !targetOK || sameDir(importerProject, targetProject) {
		return nil
	}
	directGoModule, err := directGoModuleRoot(targetProject, targetDir)
	if err != nil {
		return err
	}
	if directGoModule {
		return nil
	}
	return localImportNestedProjectError(importPath, targetProject)
}

func localImportNestedProjectError(importPath, targetProject string) error {
	return fmt.Errorf(
		"local import %q crosses nested project %s; "+
			"import it by dependency id and add project.replace for local development",
		importPath,
		targetProject,
	)
}

func directGoModuleRoot(projectDir, targetDir string) (bool, error) {
	if !sameDir(projectDir, targetDir) {
		return false, nil
	}
	marker, err := projectmarker.ClassifyDir(projectDir)
	if err != nil {
		return false, err
	}
	return marker.Kind == projectmarker.Go, nil
}

func pathWithinDir(root, target string) (bool, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return false, err
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), nil
}

func nearestProjectDir(start string) (string, bool, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false, err
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		hasProject, err := dirHasProject(dir)
		if err != nil {
			if unreadableDirectory(dir, err) {
				return "", false, nil
			}
			return "", false, err
		}
		if hasProject {
			return dir, true, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

func dirHasProject(dir string) (bool, error) {
	marker, err := projectmarker.ClassifyDir(dir)
	if err != nil {
		return false, err
	}
	return marker.Kind != projectmarker.None, nil
}

func unreadableDirectory(dir string, err error) bool {
	if !errors.Is(err, fs.ErrPermission) {
		return false
	}
	_, readErr := os.ReadDir(dir)
	return errors.Is(readErr, fs.ErrPermission)
}

func sameDir(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
}

func localSourceFromPath(importPath, abs string) (*Source, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local import %q: not a directory", importPath)
	}
	return &Source{FS: os.DirFS(abs), Path: abs}, nil
}

func addLocalProjectMetadata(source *Source, importerRoot string) error {
	projectPath, ok, err := nearestProjectDir(importerRoot)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	sourcePath, err := filepath.Abs(source.Path)
	if err != nil {
		return err
	}
	packageSubdir, err := filepath.Rel(projectPath, sourcePath)
	if err != nil {
		return err
	}
	source.ProjectFS = os.DirFS(projectPath)
	source.ProjectPath = projectPath
	source.PackageSubdir = cleanPackageSubdir(filepath.ToSlash(packageSubdir))
	return nil
}

func preserveLocalProjectMetadata(child, parent *Source, importPath string) {
	if child == nil || parent == nil || parent.ProjectFS == nil {
		return
	}
	child.Commit = parent.Commit
	child.ProjectFS = parent.ProjectFS
	child.ProjectPath = parent.ProjectPath
	child.ProjectSubdir = parent.ProjectSubdir
	child.PackageSubdir = childPackageSubdir(parent.PackageSubdir, importPath)
	child.ModuleRootPath = parent.ModuleRootPath
	child.ModulePath = parent.ModulePath
	if parent.GoImportPath != "" {
		child.GoImportPath = pathpkg.Join(parent.GoImportPath, filepath.ToSlash(importPath))
	}
}

func childPackageSubdir(parentSubdir, importPath string) string {
	return cleanPackageSubdir(pathpkg.Join(parentSubdir, filepath.ToSlash(importPath)))
}

func cleanPackageSubdir(value string) string {
	if value == "." {
		return ""
	}
	return value
}
