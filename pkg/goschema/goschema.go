// Package goschema reads a Go library's source to learn the output
// schema of each registered resource, data source, and action. The
// dev CLI feeds the result into the reference checker so trailing
// field names in references like `resource.aws.vpc.main.id` can be
// validated at compile time.
package goschema

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// Read parses the Go library rooted at dir and returns its schema
// plus any warnings about registered types whose sibling Output
// struct could not be located. Returns an error when no `Library()`
// function is found in dir's root package, or when the directory
// cannot be read.
func Read(dir string) (*runtime.LibrarySchema, []string, error) {
	rootPkg, err := parsePackageDir(dir)
	if err != nil {
		return nil, nil, err
	}
	libraryFunc := findModuleFunc(rootPkg)
	if libraryFunc == nil {
		return nil, nil, fmt.Errorf("no Library() function in %s", dir)
	}
	modulePath := readGoModPath(dir)

	schema := &runtime.LibrarySchema{
		Resources:   map[string]*runtime.TypeSchema{},
		DataSources: map[string]*runtime.TypeSchema{},
		Actions:     map[string]*runtime.TypeSchema{},
	}
	var warnings []string

	cache := map[string][]*ast.File{}
	errs := &[]error{}
	for _, reg := range extractRegistrations(libraryFunc) {
		w := newWalker(dir, modulePath, rootPkg, cache, errs)
		inputs, sensitiveIn := w.lookupFields(reg.InputRef)
		w = newWalker(dir, modulePath, rootPkg, cache, errs)
		outputs, sensitiveOut := w.lookupFields(reg.OutputRef)
		if outputs == nil {
			warnings = append(warnings, fmt.Sprintf(
				"%s %q: %s not found in the library's source",
				registrationKindLabel(reg.Field), reg.Name, reg.OutputRef.TypeName))
		}
		w = newWalker(dir, modulePath, rootPkg, cache, errs)
		constraints := w.lookupConstraints(reg.InputRef)
		ts := &runtime.TypeSchema{
			Inputs:           inputs,
			Outputs:          outputs,
			SensitiveInputs:  sortedKeys(sensitiveIn),
			SensitiveOutputs: sortedKeys(sensitiveOut),
			Constraints:      constraints,
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
	if len(*errs) > 0 {
		return nil, warnings, errors.Join(*errs...)
	}
	return schema, warnings, nil
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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

// registration is one entry extracted from the Library() function's
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

// walker carries the state needed to resolve a Go type expression
// into a typecheck.Type, including the cross-package recursion that
// follows selector types into sibling packages within the same
// library.
//
// Per-package fields (importPath, files, imports) describe the
// package the walker is currently resolving inside. Cross-package
// recursion clones the walker via sub(), swapping these fields to
// point at the target package while keeping the shared fields
// (rootDir, modulePath, packageCache, visiting) intact. visiting
// is keyed `<importPath>.<typeName>` so a recursive type that runs
// through two packages is still broken at re-entry.
type walker struct {
	rootDir      string
	modulePath   string
	packageCache map[string][]*ast.File
	visiting     map[string]bool
	errs         *[]error

	importPath string
	files      []*ast.File
	imports    map[string]string
}

func newWalker(
	rootDir, modulePath string,
	rootFiles []*ast.File,
	cache map[string][]*ast.File,
	errs *[]error,
) *walker {
	if modulePath != "" {
		cache[modulePath] = rootFiles
	}
	return &walker{
		rootDir:      rootDir,
		modulePath:   modulePath,
		packageCache: cache,
		visiting:     map[string]bool{},
		errs:         errs,
		importPath:   modulePath,
		files:        rootFiles,
		imports:      buildImportMap(rootFiles),
	}
}

// sub returns a walker positioned at the named in-library package, or
// nil when the import path lives outside the library or the
// subpackage cannot be parsed. The shared maps (packageCache,
// visiting) are aliased into the returned walker so cycle detection
// and cache hits span the whole walk.
func (w *walker) sub(importPath string) *walker {
	files, ok := w.loadPackage(importPath)
	if !ok {
		return nil
	}
	cp := *w
	cp.importPath = importPath
	cp.files = files
	cp.imports = buildImportMap(files)
	return &cp
}

// loadPackage returns the AST files for an in-library import path,
// parsing the directory lazily and caching the result. Imports
// outside the library return (nil, false).
func (w *walker) loadPackage(importPath string) ([]*ast.File, bool) {
	if files, ok := w.packageCache[importPath]; ok {
		return files, true
	}
	if w.modulePath == "" || !strings.HasPrefix(importPath, w.modulePath) {
		return nil, false
	}
	rel := strings.TrimPrefix(importPath, w.modulePath)
	rel = strings.TrimPrefix(rel, "/")
	files, err := parsePackageDir(filepath.Join(w.rootDir, rel))
	if err != nil {
		return nil, false
	}
	w.packageCache[importPath] = files
	return files, true
}

// lookupFields resolves a typeRef from a registration's type
// argument into the kebab-name to typecheck.Type map of the named
// struct's fields, plus a set of field names the library marked
// sensitive via a `ub:",sensitive"` struct tag. The walker's
// current position must be the library's root package; PkgAlias
// triggers a switch into the referenced subpackage.
func (w *walker) lookupFields(ref typeRef) (map[string]typecheck.Type, map[string]bool) {
	if ref.PkgAlias == "" {
		return w.fieldsFromPackage(ref.TypeName)
	}
	importPath, ok := w.imports[ref.PkgAlias]
	if !ok {
		return nil, nil
	}
	sub := w.sub(importPath)
	if sub == nil {
		return nil, nil
	}
	return sub.fieldsFromPackage(ref.TypeName)
}

// fieldsFromPackage finds the named type in the walker's current
// package files, follows one level of alias if present (including
// across packages for selector aliases), and returns the kebab-name
// to typecheck.Type map of the resolved struct's fields along with
// the set of fields marked sensitive.
func (w *walker) fieldsFromPackage(typeName string) (map[string]typecheck.Type, map[string]bool) {
	spec := findTypeSpec(w.files, typeName)
	if spec == nil {
		return nil, nil
	}
	if spec.Assign != token.NoPos {
		switch t := spec.Type.(type) {
		case *ast.Ident:
			spec = findTypeSpec(w.files, t.Name)
		case *ast.SelectorExpr:
			pkg, ok := identName(t.X)
			if !ok {
				return nil, nil
			}
			importPath, ok := w.imports[pkg]
			if !ok {
				return nil, nil
			}
			sub := w.sub(importPath)
			if sub == nil {
				return nil, nil
			}
			return sub.fieldsFromPackage(t.Sel.Name)
		default:
			return nil, nil
		}
		if spec == nil {
			return nil, nil
		}
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok {
		return nil, nil
	}
	key := w.importPath + "." + typeName
	w.visiting[key] = true
	defer delete(w.visiting, key)
	return w.fieldsFromStruct(st)
}

// fieldsFromStruct walks one struct's fields into a kebab-name to
// Type map plus a set of names the library marked sensitive. Each
// field's Go type goes through typeFromAST so nested struct types
// in the same package expand into Object types, and types named via
// a selector into another in-library package expand the same way.
func (w *walker) fieldsFromStruct(st *ast.StructType) (map[string]typecheck.Type, map[string]bool) {
	if st.Fields == nil {
		return nil, nil
	}
	out := map[string]typecheck.Type{}
	var sensitive map[string]bool
	for _, fld := range st.Fields.List {
		t := w.typeFromAST(fld.Type)
		name, skip, isSensitive, unknown := parseUBFieldTag(fld.Tag)
		if skip {
			continue
		}
		if len(unknown) > 0 && w.errs != nil {
			for _, opt := range unknown {
				*w.errs = append(*w.errs,
					fmt.Errorf("unknown ub option %q on field %s", opt, fieldLabel(fld)))
			}
		}
		for _, fieldName := range fld.Names {
			key := name
			if key == "" {
				key = lang.PascalToKebab(fieldName.Name)
			}
			out[key] = t
			if isSensitive {
				if sensitive == nil {
					sensitive = map[string]bool{}
				}
				sensitive[key] = true
			}
		}
	}
	return out, sensitive
}

// typeFromAST converts a Go AST type expression to a typecheck.Type.
// Named struct types in the current package expand into Object
// types; selector types into sibling packages within the same
// library expand the same way via a sub-walker. Out-of-library types
// stay Unknown except for a small allowlist (time.Duration).
func (w *walker) typeFromAST(e ast.Expr) typecheck.Type {
	switch v := e.(type) {
	case *ast.Ident:
		if t, ok := primitiveFromName(v.Name); ok {
			return t
		}
		return w.namedTypeFromIdent(v.Name)
	case *ast.SelectorExpr:
		pkg, ok := identName(v.X)
		if !ok {
			return typecheck.TUnknown()
		}
		if pkg == "time" && v.Sel.Name == "Duration" {
			return typecheck.TInteger()
		}
		importPath, ok := w.imports[pkg]
		if !ok {
			return typecheck.TUnknown()
		}
		sub := w.sub(importPath)
		if sub == nil {
			return typecheck.TUnknown()
		}
		return sub.namedTypeFromIdent(v.Sel.Name)
	case *ast.StarExpr:
		return typecheck.TOptional(w.typeFromAST(v.X))
	case *ast.ArrayType:
		return typecheck.TList(w.typeFromAST(v.Elt))
	case *ast.MapType:
		keyID, ok := v.Key.(*ast.Ident)
		if !ok || keyID.Name != "string" {
			return typecheck.TUnknown()
		}
		return typecheck.TMap(w.typeFromAST(v.Value))
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
// walker's current package. Aliases and defined-but-not-struct
// types delegate to their underlying type. Struct definitions
// become Object types with each field recursively expanded; the
// visiting set, keyed by `<importPath>.<typeName>`, guards against
// cycles across packages.
func (w *walker) namedTypeFromIdent(name string) typecheck.Type {
	key := w.importPath + "." + name
	if w.visiting[key] {
		return typecheck.TUnknown()
	}
	spec := findTypeSpec(w.files, name)
	if spec == nil {
		return typecheck.TUnknown()
	}
	if spec.Assign != token.NoPos {
		return w.typeFromAST(spec.Type)
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok {
		return w.typeFromAST(spec.Type)
	}
	w.visiting[key] = true
	defer delete(w.visiting, key)
	fields, _ := w.fieldsFromStruct(st)
	out := make([]typecheck.ObjectField, 0, len(fields))
	for fname, ft := range fields {
		out = append(out, objectField(fname, ft))
	}
	return typecheck.TObject(out)
}

// objectField builds one Object field from a struct field's kebab
// name and type. A *T Go field arrives as an Optional-kind type;
// nested object fields record optionality on the ObjectField.Optional
// flag with the inner type unwrapped, matching what FromLang produces
// for an optional() declaration. Without this the inferrer's
// missing-field check treats every pointer field as required.
func objectField(name string, t typecheck.Type) typecheck.ObjectField {
	if t.Kind == typecheck.Optional {
		return typecheck.ObjectField{Name: name, Type: t.Unwrap(), Optional: true}
	}
	return typecheck.ObjectField{Name: name, Type: t}
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
			if fn.Name.Name == "Library" {
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

// unwrapModuleLiteral takes the expression in `return &runtime.Library{...}`
// and returns the composite literal. Accepts `&Library{...}` or
// `&pkg.Library{...}` (with or without the address-of); returns nil when
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

// buildImportMap returns alias -> import path for a package's files.
// Aliases default to the last segment of the import path; an
// explicit `import x "..."` overrides. Dot and blank imports are
// skipped. When the same alias is bound to multiple paths across
// files (rare but legal at the Go level), the first binding wins;
// this is a pragmatic simplification rather than a per-file map.
func buildImportMap(files []*ast.File) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			alias := ""
			if imp.Name != nil {
				if imp.Name.Name == "." || imp.Name.Name == "_" {
					continue
				}
				alias = imp.Name.Name
			} else {
				alias = path[strings.LastIndex(path, "/")+1:]
			}
			if _, exists := out[alias]; exists {
				continue
			}
			out[alias] = path
		}
	}
	return out
}

// parseUBFieldTag reads a field's `ub` struct tag and returns its
// name (empty means use the kebab-cased field name), whether the
// field is skipped (`ub:"-"`), whether it is marked sensitive, and
// any options that are not part of the library-field contract.
// sensitive is the only option the schema acts on; omitempty and
// squash are valid codec options and pass silently; anything else is
// reported so a typo like "sensitiv" cannot quietly leave a secret
// unmasked.
func parseUBFieldTag(tag *ast.BasicLit) (name string, skip, sensitive bool, unknown []string) {
	if tag == nil {
		return "", false, false, nil
	}
	val := reflect.StructTag(strings.Trim(tag.Value, "`")).Get("ub")
	if val == "-" {
		return "", true, false, nil
	}
	parts := strings.Split(val, ",")
	name = strings.TrimSpace(parts[0])
	for _, opt := range parts[1:] {
		switch strings.TrimSpace(opt) {
		case "sensitive":
			sensitive = true
		case "omitempty", "squash", "":
		default:
			unknown = append(unknown, strings.TrimSpace(opt))
		}
	}
	return name, false, sensitive, unknown
}

// fieldLabel names a struct field for an error message, using the
// first declared name or "embedded" for an embedded field.
func fieldLabel(fld *ast.Field) string {
	if len(fld.Names) > 0 {
		return fld.Names[0].Name
	}
	return "embedded"
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
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module"))
		}
	}
	return ""
}
