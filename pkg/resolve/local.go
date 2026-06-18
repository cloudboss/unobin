package resolve

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
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
	abs := li.Path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(r.Root, li.Path)
	}
	if err := checkLocalProjectBoundary(r.Root, abs, li.Path); err != nil {
		return nil, err
	}
	return localSourceFromPath(li.Path, abs)
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
		abs := li.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(parent.Path, li.Path)
		}
		if err := checkLocalProjectBoundary(parent.Path, abs, li.Path); err != nil {
			return nil, err
		}
		return localSourceFromPath(li.Path, abs)
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
	return &Source{FS: sub}, nil
}

func checkLocalProjectBoundary(importerDir, targetDir, importPath string) error {
	importerProject, importerOK, err := nearestManifestDir(importerDir)
	if err != nil {
		return err
	}
	targetProject, targetOK, err := nearestManifestDir(targetDir)
	if err != nil {
		return err
	}
	if importerOK && targetOK && !sameDir(importerProject, targetProject) {
		return fmt.Errorf(
			"local import %q targets a different project; "+
				"import it by dependency id and add manifest.replace for local development",
			importPath,
		)
	}
	return nil
}

func nearestManifestDir(start string) (string, bool, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false, err
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		hasManifest, err := dirHasManifest(dir)
		if err != nil {
			return "", false, err
		}
		if hasManifest {
			return dir, true, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

func dirHasManifest(dir string) (bool, error) {
	path := filepath.Join(dir, "manifest.ub")
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	f, err := syntax.ParseSource(path, b)
	if err != nil {
		return false, err
	}
	if f.Kind != syntax.FileManifest || f.Manifest == nil {
		return false, fmt.Errorf("%s must declare manifest", path)
	}
	if errs := syntax.ValidateFile(f); errs.Len() > 0 {
		return false, errs.Err()
	}
	return true, nil
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
