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

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// Read parses the Go module rooted at dir and returns its schema
// plus any warnings about registered types whose sibling Output
// struct could not be located. Returns an error when no `Module()`
// function is found in dir's root package, or when the directory
// cannot be read.
func Read(dir string) (*runtime.ModuleSchema, []string, error) {
	rootPkg, err := parsePackageDir(dir)
	if err != nil {
		return nil, nil, err
	}
	moduleFunc := findModuleFunc(rootPkg)
	if moduleFunc == nil {
		return nil, nil, fmt.Errorf("no Module() function in %s", dir)
	}
	modulePath := readGoModPath(dir)

	schema := &runtime.ModuleSchema{
		Resources:   map[string]*runtime.TypeSchema{},
		DataSources: map[string]*runtime.TypeSchema{},
		Actions:     map[string]*runtime.TypeSchema{},
	}
	var warnings []string

	for _, reg := range extractRegistrations(moduleFunc) {
		inputs := lookupFields(rootPkg, dir, modulePath, reg.InputRef)
		outputs := lookupFields(rootPkg, dir, modulePath, reg.OutputRef)
		if outputs == nil {
			warnings = append(warnings, fmt.Sprintf(
				"%s %q: %s not found in the module's source",
				registrationKindLabel(reg.Field), reg.Name, reg.OutputRef.TypeName))
		}
		ts := &runtime.TypeSchema{Inputs: inputs, Outputs: outputs}
		switch reg.Field {
		case "Resources":
			schema.Resources[reg.Name] = ts
		case "DataSources":
			schema.DataSources[reg.Name] = ts
		case "Actions":
			schema.Actions[reg.Name] = ts
		}
	}
	return schema, warnings, nil
}

func registrationKindLabel(field string) string {
	switch field {
	case "Resources":
		return "resource"
	case "DataSources":
		return "data source"
	case "Actions":
		return "action"
	}
	return field
}

// registration is one entry extracted from the Module() function's
// Resources, DataSources, or Actions map. Name is the kebab-case
// key. InputRef names the receiver type (the first type argument of
// MakeResource/MakeAction/MakeDataSource); OutputRef names the
// output struct (the second type argument). Each ref points to a
// type in the root package when PkgAlias is empty, or a subpackage
// otherwise.
type registration struct {
	Field     string // "Resources", "DataSources", "Actions"
	Name      string // kebab-case
	InputRef  typeRef
	OutputRef typeRef
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
				inputRef, outputRef, ok := refsFromMakeCall(ekv.Value)
				if !ok {
					continue
				}
				out = append(out, registration{
					Field:     fieldName,
					Name:      kebab,
					InputRef:  inputRef,
					OutputRef: outputRef,
				})
			}
		}
	}
	return out
}

// refsFromMakeCall extracts the input and output type references
// from a `runtime.MakeResource[T, Out](...)` call. It accepts the
// `With` variants too. The first type argument is the input
// (receiver) type; the second is the output type. Any leading `*`
// is stripped from either so the caller looks up the struct itself.
func refsFromMakeCall(e ast.Expr) (input, output typeRef, ok bool) {
	call, callOk := e.(*ast.CallExpr)
	if !callOk {
		return typeRef{}, typeRef{}, false
	}
	indices := indexedTypeArgs(call.Fun)
	if len(indices) < 2 {
		return typeRef{}, typeRef{}, false
	}
	input, inOk := outputTypeRef(indices[0])
	output, outOk := outputTypeRef(indices[1])
	if !outOk {
		return typeRef{}, typeRef{}, false
	}
	if !inOk {
		input = typeRef{}
	}
	return input, output, true
}

// indexedTypeArgs returns the type-argument expressions on a generic
// call's function part, in source order. The call `MakeResource[T, Out]()`
// has fn = IndexListExpr{ X: MakeResource, Indices: [T, Out] }.
// For older or single-arg shapes (`MakeResource[T]()`), it returns
// the single index.
func indexedTypeArgs(fn ast.Expr) []ast.Expr {
	switch v := fn.(type) {
	case *ast.IndexListExpr:
		return v.Indices
	case *ast.IndexExpr:
		return []ast.Expr{v.Index}
	}
	return nil
}

// outputTypeRef converts a type-argument expression like `*VpcOutput`
// or `*resources.VpcOutput` into a typeRef. A leading `*` is
// stripped.
func outputTypeRef(e ast.Expr) (typeRef, bool) {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	switch v := e.(type) {
	case *ast.Ident:
		return typeRef{TypeName: v.Name}, true
	case *ast.SelectorExpr:
		pkg, ok := identName(v.X)
		if !ok {
			return typeRef{}, false
		}
		return typeRef{PkgAlias: pkg, TypeName: v.Sel.Name}, true
	}
	return typeRef{}, false
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

// lookupFields resolves a typeRef to the kebab-name to typecheck.Type
// map of the named struct's fields. The ref's package alias (empty
// for the root package) selects which package the type lives in; a
// subpackage is parsed lazily.
func lookupFields(
	rootPkg []*ast.File, rootDir, modulePath string, ref typeRef,
) map[string]typecheck.Type {
	if ref.PkgAlias == "" {
		return fieldsFromPackage(rootPkg, ref.TypeName)
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
	return fieldsFromPackage(subPkg, ref.TypeName)
}

// fieldsFromPackage finds the named type in the package's files,
// follows one level of alias if present, and returns the kebab-name
// to typecheck.Type map of the resolved struct's fields. Same-
// package nested struct types are expanded recursively; selector
// types and unrecognized Go types collapse to Unknown.
func fieldsFromPackage(files []*ast.File, typeName string) map[string]typecheck.Type {
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
	if !ok {
		return nil
	}
	visiting := map[string]bool{typeName: true}
	return fieldsFromStruct(st, files, visiting)
}

// fieldsFromStruct walks one struct's fields into a kebab-name to
// Type map. Each field's Go type goes through typeFromAST so nested
// struct types in the same package expand into Object types.
func fieldsFromStruct(
	st *ast.StructType, files []*ast.File, visiting map[string]bool,
) map[string]typecheck.Type {
	if st.Fields == nil {
		return nil
	}
	out := map[string]typecheck.Type{}
	for _, fld := range st.Fields.List {
		t := typeFromAST(fld.Type, files, visiting)
		tag := mapstructureTag(fld.Tag)
		for _, name := range fld.Names {
			key := tag
			if key == "" {
				key = lang.PascalToKebab(name.Name)
			}
			out[key] = t
		}
	}
	return out
}

// typeFromAST converts a Go AST type expression to a typecheck.Type.
// Same-package named struct types expand into Object types; named
// aliases follow to their underlying type. Cross-package selectors
// stay Unknown except for a small allowlist (time.Duration).
// visiting tracks named types currently being walked so a recursive
// struct does not loop forever.
func typeFromAST(
	e ast.Expr, files []*ast.File, visiting map[string]bool,
) typecheck.Type {
	switch v := e.(type) {
	case *ast.Ident:
		if t, ok := primitiveFromName(v.Name); ok {
			return t
		}
		return namedTypeFromIdent(v.Name, files, visiting)
	case *ast.SelectorExpr:
		pkg, ok := identName(v.X)
		if !ok {
			return typecheck.TUnknown()
		}
		if pkg == "time" && v.Sel.Name == "Duration" {
			return typecheck.TInteger()
		}
		return typecheck.TUnknown()
	case *ast.StarExpr:
		return typecheck.TOptional(typeFromAST(v.X, files, visiting))
	case *ast.ArrayType:
		return typecheck.TList(typeFromAST(v.Elt, files, visiting))
	case *ast.MapType:
		keyID, ok := v.Key.(*ast.Ident)
		if !ok || keyID.Name != "string" {
			return typecheck.TUnknown()
		}
		return typecheck.TMap(typeFromAST(v.Value, files, visiting))
	case *ast.InterfaceType:
		return typecheck.TAny()
	}
	return typecheck.TUnknown()
}

func primitiveFromName(name string) (typecheck.Type, bool) {
	switch name {
	case "string":
		return typecheck.TString(), true
	case "bool":
		return typecheck.TBoolean(), true
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "byte", "rune":
		return typecheck.TInteger(), true
	case "float32", "float64":
		return typecheck.TNumber(), true
	case "any":
		return typecheck.TAny(), true
	}
	return typecheck.Type{}, false
}

// namedTypeFromIdent resolves an identifier that names a type in the
// same package. Aliases and defined-but-not-struct types delegate to
// their underlying type. Struct definitions become Object types with
// each field recursively expanded; the visiting set guards against
// cycles.
func namedTypeFromIdent(
	name string, files []*ast.File, visiting map[string]bool,
) typecheck.Type {
	if visiting[name] {
		return typecheck.TUnknown()
	}
	spec := findTypeSpec(files, name)
	if spec == nil {
		return typecheck.TUnknown()
	}
	if spec.Assign != token.NoPos {
		return typeFromAST(spec.Type, files, visiting)
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok {
		return typeFromAST(spec.Type, files, visiting)
	}
	visiting[name] = true
	defer delete(visiting, name)
	fields := fieldsFromStruct(st, files, visiting)
	out := make([]typecheck.ObjectField, 0, len(fields))
	for fname, ft := range fields {
		out = append(out, typecheck.ObjectField{Name: fname, Type: ft})
	}
	return typecheck.TObject(out)
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
