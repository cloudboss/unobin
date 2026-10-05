package goschema

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

func buildImportMaps(files []*ast.File, roots ...ModuleRoot) map[*ast.File]map[string]string {
	imports := make(map[*ast.File]map[string]string, len(files))
	for _, file := range files {
		imports[file] = buildImportMap(file, roots...)
	}
	return imports
}

func buildImportMap(file *ast.File, roots ...ModuleRoot) map[string]string {
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
			name = declaredImportName(importPath, roots)
		}
		imports[name] = importPath
	}
	return imports
}

func declaredImportName(importPath string, roots []ModuleRoot) string {
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
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, entry := range entries {
				name := entry.Name()
				if entry.IsDir() || !strings.HasSuffix(name, ".go") ||
					strings.HasSuffix(name, "_test.go") {
					continue
				}
				file, err := parser.ParseFile(token.NewFileSet(),
					filepath.Join(dir, name), nil, parser.PackageClauseOnly)
				if err == nil {
					return file.Name.Name
				}
			}
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
