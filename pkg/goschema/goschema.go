// Package goschema reads a Go module's source to learn the output
// schema of each registered resource, data source, and action. The
// dev CLI feeds the result into the reference checker so trailing
// field names in references like `resource.aws.vpc.main.id` can be
// validated at compile time.
//
// The convention is that each registered Go type referenced by a
// `New:` function in the module's `Module()` registration has a
// sibling Go type named `<GoName>Output` (or a type alias of one)
// whose `mapstructure` tags name the kebab-case output field keys.
package goschema

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/cloudboss/unobin/pkg/runtime"
)

// Read parses the Go module rooted at dir and returns its schema.
// Returns an error when no `Module()` function is found in dir's
// root package, or when the directory cannot be read.
func Read(dir string) (*runtime.ModuleSchema, error) {
	rootPkg, err := parsePackageDir(dir)
	if err != nil {
		return nil, err
	}
	moduleFunc := findModuleFunc(rootPkg)
	if moduleFunc == nil {
		return nil, fmt.Errorf("no Module() function in %s", dir)
	}
	modulePath := readGoModPath(dir)

	schema := &runtime.ModuleSchema{
		Resources:   map[string]*runtime.TypeSchema{},
		DataSources: map[string]*runtime.TypeSchema{},
		Actions:     map[string]*runtime.TypeSchema{},
	}

	for _, reg := range extractRegistrations(moduleFunc) {
		ts := &runtime.TypeSchema{
			Outputs: lookupOutputs(rootPkg, dir, modulePath, reg.Ref),
		}
		switch reg.Field {
		case "Resources":
			schema.Resources[reg.Name] = ts
		case "DataSources":
			schema.DataSources[reg.Name] = ts
		case "Actions":
			schema.Actions[reg.Name] = ts
		}
	}
	return schema, nil
}

// registration is one entry extracted from the Module() function's
// Resources, DataSources, or Actions map. Name is the kebab-case
// key. Ref names the Go type referenced from the New func: same
// package when PkgAlias is empty, else a type in the package
// imported under that alias.
type registration struct {
	Field    string // "Resources", "DataSources", "Actions"
	Name     string // kebab-case
	Ref      typeRef
}

type typeRef struct {
	PkgAlias string
	TypeName string
}

func parsePackageDir(dir string) ([]*ast.File, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", dir, err)
	}
	for _, pkg := range pkgs {
		files := make([]*ast.File, 0, len(pkg.Files))
		for _, f := range pkg.Files {
			files = append(files, f)
		}
		return files, nil
	}
	return nil, fmt.Errorf("no Go package found in %s", dir)
}

func findModuleFunc(files []*ast.File) *ast.FuncDecl {
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Recv != nil {
				continue
			}
			if fn.Name.Name == "Module" {
				return fn
			}
		}
	}
	return nil
}

func extractRegistrations(fn *ast.FuncDecl) []registration {
	var out []registration
	if fn.Body == nil {
		return out
	}
	for _, stmt := range fn.Body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		composite := unwrapModuleLiteral(ret.Results[0])
		if composite == nil {
			continue
		}
		for _, el := range composite.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			fieldName, ok := identName(kv.Key)
			if !ok {
				continue
			}
			if fieldName != "Resources" && fieldName != "DataSources" &&
				fieldName != "Actions" {
				continue
			}
			mapLit, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, entry := range mapLit.Elts {
				ekv, ok := entry.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				kebab, ok := stringLit(ekv.Key)
				if !ok {
					continue
				}
				entryLit, ok := ekv.Value.(*ast.CompositeLit)
				if !ok {
					continue
				}
				ref, ok := findNewRef(entryLit)
				if !ok {
					continue
				}
				out = append(out, registration{
					Field: fieldName,
					Name:  kebab,
					Ref:   ref,
				})
			}
		}
	}
	return out
}

// unwrapModuleLiteral takes the expression in `return &runtime.Module{...}`
// and returns the composite literal. Accepts `&Module{...}` or
// `&pkg.Module{...}` (with or without the address-of); returns nil when
// the shape doesn't match.
func unwrapModuleLiteral(e ast.Expr) *ast.CompositeLit {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	return cl
}

// findNewRef walks the body of a registration entry like
// `{Name: "x", New: func() runtime.X { return &<expr>{} }}` and
// returns the Go type that the New func instantiates.
func findNewRef(entry *ast.CompositeLit) (typeRef, bool) {
	for _, el := range entry.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, ok := identName(kv.Key)
		if !ok || name != "New" {
			continue
		}
		fl, ok := kv.Value.(*ast.FuncLit)
		if !ok || fl.Body == nil {
			continue
		}
		for _, stmt := range fl.Body.List {
			ret, ok := stmt.(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}
			expr := ret.Results[0]
			if u, ok := expr.(*ast.UnaryExpr); ok && u.Op == token.AND {
				expr = u.X
			}
			cl, ok := expr.(*ast.CompositeLit)
			if !ok {
				continue
			}
			switch t := cl.Type.(type) {
			case *ast.Ident:
				return typeRef{TypeName: t.Name}, true
			case *ast.SelectorExpr:
				pkg, ok := identName(t.X)
				if !ok {
					return typeRef{}, false
				}
				return typeRef{PkgAlias: pkg, TypeName: t.Sel.Name}, true
			}
		}
	}
	return typeRef{}, false
}

func lookupOutputs(
	rootPkg []*ast.File, rootDir, modulePath string, ref typeRef,
) map[string]string {
	outName := ref.TypeName + "Output"
	if ref.PkgAlias == "" {
		return outputsFromPackage(rootPkg, outName)
	}
	importPath := resolveImportPath(rootPkg, ref.PkgAlias)
	if importPath == "" || modulePath == "" ||
		!strings.HasPrefix(importPath, modulePath) {
		return nil
	}
	rel := strings.TrimPrefix(importPath, modulePath)
	rel = strings.TrimPrefix(rel, "/")
	subPkg, err := parsePackageDir(filepath.Join(rootDir, rel))
	if err != nil {
		return nil
	}
	return outputsFromPackage(subPkg, outName)
}

// outputsFromPackage finds the named type in the package's files,
// follows one level of alias if present, and returns the kebab-tag
// to Go-type map extracted from the resolved struct's fields.
func outputsFromPackage(files []*ast.File, typeName string) map[string]string {
	spec := findTypeSpec(files, typeName)
	if spec == nil {
		return nil
	}
	if spec.Assign != token.NoPos {
		switch t := spec.Type.(type) {
		case *ast.Ident:
			spec = findTypeSpec(files, t.Name)
		case *ast.SelectorExpr:
			return nil
		}
		if spec == nil {
			return nil
		}
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return nil
	}
	out := map[string]string{}
	for _, fld := range st.Fields.List {
		tag := mapstructureTag(fld.Tag)
		if tag == "" {
			continue
		}
		typeStr := typeString(fld.Type)
		out[tag] = typeStr
	}
	return out
}

func findTypeSpec(files []*ast.File, name string) *ast.TypeSpec {
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if ts.Name.Name == name {
					return ts
				}
			}
		}
	}
	return nil
}

func resolveImportPath(files []*ast.File, alias string) string {
	for _, f := range files {
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			name := alias
			if imp.Name != nil {
				if imp.Name.Name == alias {
					return path
				}
				continue
			}
			// No explicit alias: the imported package name is the
			// last path segment by convention.
			seg := path[strings.LastIndex(path, "/")+1:]
			if seg == name {
				return path
			}
		}
	}
	return ""
}

func mapstructureTag(tag *ast.BasicLit) string {
	if tag == nil {
		return ""
	}
	raw := strings.Trim(tag.Value, "`")
	t := reflect.StructTag(raw)
	return t.Get("mapstructure")
}

// typeString stringifies an AST type expression in the form used by
// the project's Go source.
func typeString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		x, _ := identName(v.X)
		return x + "." + v.Sel.Name
	case *ast.StarExpr:
		return "*" + typeString(v.X)
	case *ast.ArrayType:
		return "[]" + typeString(v.Elt)
	case *ast.MapType:
		return "map[" + typeString(v.Key) + "]" + typeString(v.Value)
	case *ast.InterfaceType:
		return "any"
	}
	return "?"
}

func identName(e ast.Expr) (string, bool) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := unquoteString(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func unquoteString(s string) (string, error) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1], nil
	}
	return "", fmt.Errorf("not a double-quoted string: %s", s)
}

// readGoModPath reads the `module <path>` declaration from a go.mod
// at dir. Returns "" when the file is missing or malformed; callers
// fall back to treating subpackage references as unresolvable.
func readGoModPath(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module"))
		}
	}
	return ""
}
