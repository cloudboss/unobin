package resolve

import (
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
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
