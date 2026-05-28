package resolve

import (
	"fmt"
	"os"
	"path/filepath"
)

// LocalResolver resolves *LocalImport refs against a working directory
// root. Relative paths in the import are joined to Root.
type LocalResolver struct {
	Root string
}

// NewLocalResolver returns a LocalResolver rooted at root. Pass the
// directory containing the factory.ub or library.ub that owns the imports.
func NewLocalResolver(root string) *LocalResolver {
	return &LocalResolver{Root: root}
}

// Resolve implements Resolver. The ref must be a *LocalImport; remote
// refs return an error so a misrouted call surfaces clearly.
func (r *LocalResolver) Resolve(ref ImportRef) (*Source, error) {
	li, ok := ref.(*LocalImport)
	if !ok {
		return nil, fmt.Errorf("local resolver cannot handle %T", ref)
	}
	abs := li.Path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(r.Root, li.Path)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local import %q: not a directory", li.Path)
	}
	return &Source{FS: os.DirFS(abs), Path: abs}, nil
}
