package goschema

import (
	"go/ast"
	"go/token"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/gopackage"
)

func buildImportMaps(
	files []*ast.File, target gopackage.Context, roots ...ModuleRoot,
) map[*ast.File]map[string]string {
	imports := make(map[*ast.File]map[string]string, len(files))
	for _, file := range files {
		imports[file] = buildImportMap(file, target, roots...)
	}
	return imports
}

func buildImportMap(
	file *ast.File, target gopackage.Context, roots ...ModuleRoot,
) map[string]string {
	imports := make(map[string]string, len(file.Imports))
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
			if name == "." || name == "_" {
				continue
			}
		} else {
			name = declaredImportName(importPath, target, roots)
		}
		imports[name] = importPath
	}
	return imports
}

func declaredImportName(importPath string, target gopackage.Context, roots []ModuleRoot) string {
	var selected ModuleRoot
	for _, root := range roots {
		if importPath != root.Path && !strings.HasPrefix(importPath, root.Path+"/") {
			continue
		}
		if len(root.Path) > len(selected.Path) {
			selected = root
		}
	}
	if selected.Dir != "" {
		rel := strings.TrimPrefix(strings.TrimPrefix(importPath, selected.Path), "/")
		dir := filepath.Join(selected.Dir, filepath.FromSlash(rel))
		pkg, err := gopackage.Load(dir, target)
		if err == nil {
			return pkg.Name
		}
	}
	return path.Base(importPath)
}

func (p *indexedPackage) importPathFor(alias string, pos token.Pos) string {
	for _, file := range p.files {
		if pos >= file.Pos() && pos <= file.End() {
			return p.imports[file][alias]
		}
	}
	return ""
}

func (w *walker) importPathFor(alias string, pos token.Pos) string {
	return w.pkg.importPathFor(alias, pos)
}
