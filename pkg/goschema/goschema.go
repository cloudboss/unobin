// Package goschema reads a Go library's source to learn the output
// schema of each registered resource, data source, and action. The
// dev CLI feeds the result into the reference checker so trailing
// field names in references like `resource.app.id` can be validated at
// compile time.
package goschema

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"os"
	pathpkg "path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/encoding/ub"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// Analysis is the complete Go library source analysis.
type Analysis struct {
	Schema   *runtime.LibrarySchema
	Index    *SourceIndex
	Warnings []string
}

// Analyze reads a Go library's source and returns its schema facts and locations.
func Analyze(dir string, extra ...ModuleRoot) (*Analysis, error) {
	ctx, err := newAnalysisContext(dir, extra...)
	if err != nil {
		return nil, err
	}
	return ctx.run()
}

// Read parses the Go library rooted at dir and returns its schema
// plus any warnings: registered types whose sibling Output struct
// could not be located, and constraints whose pieces could not be
// extracted from source. Returns an error when no `Library()`
// function is found in dir's root package, or when the directory
// cannot be read. extra lists further module roots the walker may
// read source from when a referenced type lives outside the
// library's own module.
func Read(dir string, extra ...ModuleRoot) (*runtime.LibrarySchema, []string, error) {
	analysis, err := Analyze(dir, extra...)
	if err != nil {
		if analysis == nil {
			return nil, nil, err
		}
		return analysis.Schema, analysis.Warnings, err
	}
	return analysis.Schema, analysis.Warnings, nil
}

// ReadLibraryConfiguration parses a package's LibraryConfiguration entry point.
func ReadLibraryConfiguration(
	dir string,
	extra ...ModuleRoot,
) (*runtime.LibrarySchema, []string, error) {
	ctx, err := newAnalysisContext(dir, extra...)
	if err != nil {
		return nil, nil, err
	}
	fn := findPackageFunc(ctx.root.files, "LibraryConfiguration")
	if fn == nil {
		return nil, nil, fmt.Errorf("no LibraryConfiguration() function in %s", dir)
	}
	schema, err := ctx.readLibraryConfigurationSchema(fn)
	if err != nil {
		return schema, slices.Clone(ctx.warnings), err
	}
	return schema, slices.Clone(ctx.warnings), nil
}

type analysisContext struct {
	dir      string
	roots    []ModuleRoot
	root     *indexedPackage
	packages map[string]*indexedPackage
	warnings []string
	errs     []error
}

func newAnalysisContext(dir string, extra ...ModuleRoot) (*analysisContext, error) {
	root, importPath := packageModuleRoot(dir)
	rootPkg, err := parseIndexedPackageDir(dir, importPath)
	if err != nil {
		return nil, err
	}
	roots := make([]ModuleRoot, 0, 1+len(extra))
	roots = append(roots, root)
	roots = append(roots, extra...)
	packages := map[string]*indexedPackage{}
	if rootPkg.importPath != "" {
		packages[rootPkg.importPath] = rootPkg
	}
	return &analysisContext{
		dir:      dir,
		roots:    roots,
		root:     rootPkg,
		packages: packages,
	}, nil
}

func (c *analysisContext) run() (*Analysis, error) {
	libraryFunc := findModuleFunc(c.root.files)
	if libraryFunc == nil {
		return nil, fmt.Errorf("no Library() function in %s", c.dir)
	}
	schema, err := c.readSchema(libraryFunc)
	if err != nil {
		return &Analysis{Warnings: slices.Clone(c.warnings)}, err
	}
	index, err := c.buildSourceIndex(libraryFunc)
	if err != nil {
		return &Analysis{Schema: schema, Warnings: slices.Clone(c.warnings)}, err
	}
	return &Analysis{
		Schema:   schema,
		Index:    index,
		Warnings: slices.Clone(c.warnings),
	}, nil
}

func (c *analysisContext) readSchema(
	libraryFunc *ast.FuncDecl,
) (*runtime.LibrarySchema, error) {
	schema := &runtime.LibrarySchema{
		Resources:   map[string]*runtime.TypeSchema{},
		DataSources: map[string]*runtime.TypeSchema{},
		Actions:     map[string]*runtime.TypeSchema{},
		Functions:   map[string]typecheck.FuncSig{},
	}

	for _, reg := range extractRegistrations(libraryFunc) {
		kind := registrationKindLabel(reg.Field)
		w := c.newWalker()
		w.subject = fmt.Sprintf("%s %q input", kind, reg.Name)
		inputs, sensitiveIn := w.lookupFields(reg.InputRef)
		w = c.newWalker()
		w.subject = fmt.Sprintf("%s %q output", kind, reg.Name)
		outputs, sensitiveOut := w.lookupFields(reg.OutputRef)
		if outputs == nil {
			c.warnings = append(c.warnings, fmt.Sprintf(
				"%s %q: %s not found in reachable source",
				registrationKindLabel(reg.Field), reg.Name, reg.OutputRef))
		}
		w = c.newWalker()
		constraints := w.lookupConstraints(reg.InputRef)
		w = c.newWalker()
		defaultSpecs := w.lookupDefaults(reg.InputRef)
		ts := &runtime.TypeSchema{
			Inputs:           inputs,
			Outputs:          outputs,
			SensitiveInputs:  sortedKeys(sensitiveIn),
			SensitiveOutputs: sortedKeys(sensitiveOut),
			Constraints:      constraints,
			Defaults:         defaultSpecs,
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
	if ref, init, found, ok := extractConfigurationRef(
		libraryFunc,
		c.root,
		c.loadImportedPackage,
	); found {
		schema.HasConfiguration = true
		if !ok {
			c.warnings = append(c.warnings,
				"library configuration: cannot read the struct behind New from source "+
					"(write `New: func() any { return &T{} }`), so configuration fields "+
					"are unchecked")
		} else {
			c.fillConfigurationSchema(schema, ref, init, "library configuration")
		}
	}
	if err := c.checkLibraryConfigurationMatch(schema); err != nil {
		return nil, err
	}
	maps.Copy(schema.Functions, extractFunctions(libraryFunc, c.root.files, &c.errs))
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	return schema, nil
}

func (c *analysisContext) readLibraryConfigurationSchema(
	fn *ast.FuncDecl,
) (*runtime.LibrarySchema, error) {
	schema := &runtime.LibrarySchema{
		Resources:        map[string]*runtime.TypeSchema{},
		DataSources:      map[string]*runtime.TypeSchema{},
		Actions:          map[string]*runtime.TypeSchema{},
		Functions:        map[string]typecheck.FuncSig{},
		HasConfiguration: true,
	}
	ref, init, found, ok := extractLibraryConfigurationRef(
		fn,
		c.root,
		c.loadImportedPackage,
	)
	if !found || !ok {
		return schema, fmt.Errorf(
			"LibraryConfiguration(): cannot read the struct behind New from source")
	}
	if !c.fillConfigurationSchema(schema, ref, init, "library configuration") {
		return schema, fmt.Errorf(
			"LibraryConfiguration(): %s not found in reachable source", ref)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	return schema, nil
}

func (c *analysisContext) checkLibraryConfigurationMatch(
	schema *runtime.LibrarySchema,
) error {
	if schema == nil || !schema.HasConfiguration ||
		(schema.ConfigurationFields == nil && !schema.ConfigurationEmpty) {
		return nil
	}
	fn := findPackageFunc(c.root.files, "LibraryConfiguration")
	if fn == nil {
		return nil
	}
	ref, init, found, ok := extractLibraryConfigurationRef(
		fn,
		c.root,
		c.loadImportedPackage,
	)
	if !found || !ok {
		return fmt.Errorf(
			"LibraryConfiguration(): cannot read the struct behind New from source")
	}
	other := &runtime.LibrarySchema{HasConfiguration: true}
	if !c.fillConfigurationSchema(other, ref, init, "LibraryConfiguration()") {
		return fmt.Errorf(
			"LibraryConfiguration(): %s not found in reachable source", ref)
	}
	if schema.ConfigurationIdentity == other.ConfigurationIdentity &&
		schema.ConfigurationDigest == other.ConfigurationDigest {
		return nil
	}
	return fmt.Errorf("LibraryConfiguration() disagrees with Library().Configuration")
}

func (c *analysisContext) fillConfigurationSchema(
	schema *runtime.LibrarySchema,
	ref typeRef,
	init *ast.CompositeLit,
	subject string,
) bool {
	w := c.newWalker()
	w.subject = subject
	fields, defaults, _ := w.lookupConfigurationFields(ref, init)
	if fields == nil {
		c.warnings = append(c.warnings, fmt.Sprintf(
			"%s: %s not found in reachable source, so configuration fields are unchecked",
			subject, ref))
		return false
	}
	w = c.newWalker()
	constraints := w.lookupConstraints(ref)
	schema.Configuration = objectFieldsToMap(fields)
	schema.ConfigurationFields = fields
	schema.ConfigurationDefaults = defaults
	schema.ConfigurationConstraints = constraints
	schema.ConfigurationIdentity = c.configurationIdentity(ref)
	schema.ConfigurationEmpty = len(fields) == 0
	schema.ConfigurationDigest = cfg.DigestView(fields, defaults, constraints)
	return true
}

func (c *analysisContext) configurationIdentity(ref typeRef) string {
	importPath := ref.ImportPath
	if importPath == "" {
		importPath = c.root.importPath
	}
	if ref.PkgAlias != "" {
		importPath = c.root.imports[ref.PkgAlias]
	}
	if importPath == "" {
		return ref.String()
	}
	return importPath + "." + ref.TypeName
}

func (c *analysisContext) newWalker() *walker {
	return newWalker(c.roots, c.root, c.packages, &c.errs, &c.warnings)
}

func (c *analysisContext) buildSourceIndex(libraryFunc *ast.FuncDecl) (*SourceIndex, error) {
	indexer := &sourceIndexer{
		roots:    c.roots,
		packages: c.packages,
		root:     c.root,
	}
	return indexer.build(libraryFunc)
}

func (c *analysisContext) buildLibraryConfigurationSourceIndex(
	fn *ast.FuncDecl,
) (*SourceIndex, error) {
	indexer := &sourceIndexer{
		roots:    c.roots,
		packages: c.packages,
		root:     c.root,
	}
	return indexer.buildLibraryConfiguration(fn)
}

func (c *analysisContext) loadImportedPackage(
	from *indexedPackage,
	alias string,
) (*indexedPackage, bool) {
	if from == nil {
		return nil, false
	}
	importPath, ok := from.imports[alias]
	if !ok {
		return nil, false
	}
	w := c.newWalker()
	files, ok := w.loadPackage(importPath)
	if !ok {
		return nil, false
	}
	pkg := c.packages[importPath]
	if pkg == nil {
		pkg = &indexedPackage{
			files:      files,
			importPath: importPath,
			imports:    buildImportMap(files),
		}
		c.packages[importPath] = pkg
	}
	return pkg, true
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
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
	PkgAlias   string
	ImportPath string
	TypeName   string
}

// String renders the ref with a qualifier when the type lives in another
// package.
func (r typeRef) String() string {
	if r.PkgAlias != "" {
		return r.PkgAlias + "." + r.TypeName
	}
	if r.ImportPath != "" {
		return r.ImportPath + "." + r.TypeName
	}
	return r.TypeName
}

// ModuleRoot names a module's source on disk: Path is the module's
// import path and Dir is the directory holding its source. The walker
// resolves a selector into any root whose Path contains the selector's
// import path.
type ModuleRoot struct {
	Path string
	Dir  string
}

// walker holds the state needed to resolve a Go type expression into
// a typecheck.Type, including the cross-package recursion that follows
// selector types into other packages under the walker's module roots.
//
// Per-package fields (importPath, files, imports) describe the
// package the walker is currently resolving inside. Cross-package
// recursion clones the walker via sub(), swapping these fields to
// point at the target package while keeping the shared fields
// (roots, packageCache, visiting) intact. visiting is keyed
// `<importPath>.<typeName>` so a recursive type that runs through
// two packages is still broken at re-entry.
type walker struct {
	roots        []ModuleRoot
	packageCache map[string]*indexedPackage
	visiting     map[string]bool
	errs         *[]error
	warns        *[]string

	// subject names what the walker is extracting for diagnostics, the
	// input type whose Constraints method is being read.
	subject string

	importPath string
	files      []*ast.File
	imports    map[string]string
}

// newWalker positions a walker at the package being read. Packages under the
// module roots become readable through cross-package recursion.
func newWalker(
	roots []ModuleRoot,
	rootPkg *indexedPackage,
	cache map[string]*indexedPackage,
	errs *[]error,
	warns *[]string,
) *walker {
	if rootPkg.importPath != "" {
		cache[rootPkg.importPath] = rootPkg
	}
	return &walker{
		roots:        roots,
		packageCache: cache,
		visiting:     map[string]bool{},
		errs:         errs,
		warns:        warns,
		importPath:   rootPkg.importPath,
		files:        rootPkg.files,
		imports:      buildImportMap(rootPkg.files),
	}
}

// sub returns a walker positioned at the named package under one of
// the module roots, or nil when the import path lives outside every
// root or the package cannot be parsed. The shared maps (packageCache,
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

// loadPackage returns the AST files for an import path under one of
// the module roots, parsing the directory lazily and caching the
// result. Imports outside every root return (nil, false).
func (w *walker) loadPackage(importPath string) ([]*ast.File, bool) {
	if pkg, ok := w.packageCache[importPath]; ok {
		return pkg.files, true
	}
	root, ok := w.rootFor(importPath)
	if !ok {
		return nil, false
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(importPath, root.Path), "/")
	pkg, err := parseIndexedPackageDir(filepath.Join(root.Dir, rel), importPath)
	if err != nil {
		return nil, false
	}
	w.packageCache[importPath] = pkg
	return pkg.files, true
}

// rootFor finds the module root containing importPath, preferring the
// longest matching path so a nested module wins over one whose path
// contains it. A root matches when its path equals the import path or
// is a path-segment prefix of it.
func (w *walker) rootFor(importPath string) (ModuleRoot, bool) {
	var best ModuleRoot
	found := false
	for _, root := range w.roots {
		if root.Path == "" {
			continue
		}
		if importPath != root.Path && !strings.HasPrefix(importPath, root.Path+"/") {
			continue
		}
		if !found || len(root.Path) > len(best.Path) {
			best = root
			found = true
		}
	}
	return best, found
}

// lookupFields resolves a typeRef from a registration's type
// argument into the kebab-name to typecheck.Type map of the named
// struct's fields, plus a set of field names the library marked
// sensitive via a `ub:",sensitive"` struct tag. The walker's
// current position must be the library's root package; PkgAlias
// triggers a switch into the referenced subpackage.
func (w *walker) lookupFields(ref typeRef) (map[string]typecheck.Type, map[string]bool) {
	fields, sensitive := w.lookupObjectFields(ref)
	return objectFieldsToMap(fields), sensitive
}

func (w *walker) lookupObjectFields(ref typeRef) ([]typecheck.ObjectField, map[string]bool) {
	cw := w.walkerForRef(ref)
	if cw == nil {
		return nil, nil
	}
	return cw.objectFieldsFromPackage(ref.TypeName)
}

func (w *walker) walkerForRef(ref typeRef) *walker {
	importPath := ref.ImportPath
	if ref.PkgAlias != "" {
		var ok bool
		importPath, ok = w.imports[ref.PkgAlias]
		if !ok {
			return nil
		}
	}
	if importPath == "" || importPath == w.importPath {
		return w
	}
	return w.sub(importPath)
}

// objectFieldsFromPackage finds the named type in the walker's current
// package files, follows one level of alias if present (including
// across packages for selector aliases), and returns fields in declaration
// order with the set of fields marked sensitive.
func (w *walker) objectFieldsFromPackage(
	typeName string,
) ([]typecheck.ObjectField, map[string]bool) {
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
			return sub.objectFieldsFromPackage(t.Sel.Name)
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
	return w.objectFieldsFromStruct(st)
}

// objectFieldsFromStruct walks one struct's fields in declaration order.
func (w *walker) objectFieldsFromStruct(
	st *ast.StructType,
) ([]typecheck.ObjectField, map[string]bool) {
	if st.Fields == nil {
		return nil, nil
	}
	fields := []typecheck.ObjectField{}
	var sensitive map[string]bool
	w.eachStructField(st, func(kebab string, fld *ast.Field, isSensitive bool) {
		t := w.typeFromAST(fld.Type)
		if t.ContainsUnknown() {
			w.addWarnf("field %s: Go type %s does not fully map to language types, "+
				"so reads of it are unchecked", kebab, types.ExprString(fld.Type))
		}
		fields = append(fields, objectField(kebab, t))
		if isSensitive {
			if sensitive == nil {
				sensitive = map[string]bool{}
			}
			sensitive[kebab] = true
		}
	})
	return fields, sensitive
}

func objectFieldsToMap(fields []typecheck.ObjectField) map[string]typecheck.Type {
	if fields == nil {
		return nil
	}
	out := make(map[string]typecheck.Type, len(fields))
	for _, field := range fields {
		t := field.Type
		if field.Optional {
			t = typecheck.TOptional(t)
		}
		out[field.Name] = t
	}
	return out
}

// eachStructField calls fn for every declared, unskipped field of st
// in declaration order, with the field's kebab input name and its
// sensitivity. A field declaring several names is visited once per
// name, in order. Unknown ub tag options are reported as errors here,
// once per field.
func (w *walker) eachStructField(
	st *ast.StructType, fn func(kebab string, fld *ast.Field, sensitive bool),
) {
	if st.Fields == nil {
		return
	}
	for _, fld := range st.Fields.List {
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
			kebab := name
			if kebab == "" {
				kebab = lang.PascalToKebab(fieldName.Name)
			}
			fn(kebab, fld, isSensitive)
		}
	}
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
		if importPath == cfgPkgPath {
			if t, ok := cfgScalarType(v.Sel.Name); ok {
				return t
			}
			return typecheck.TUnknown()
		}
		sub := w.sub(importPath)
		if sub == nil {
			return typecheck.TUnknown()
		}
		return sub.namedTypeFromIdent(v.Sel.Name)
	case *ast.IndexExpr:
		return w.genericTypeFromAST(v.X, v.Index)
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
		return typecheck.TOpaque()
	}
	return typecheck.TUnknown()
}

// cfgPkgPath is the import path of the SDK's configuration value
// types. Struct fields of these types map directly to language types,
// unlike other out-of-library types, which stay Unknown.
const cfgPkgPath = "github.com/cloudboss/unobin/pkg/sdk/cfg"

// cfgScalarType maps a named scalar type in the cfg package to its
// language type. List, Map, and Object are generic and resolve in
// genericTypeFromAST, where the type argument is in hand.
func cfgScalarType(name string) (typecheck.Type, bool) {
	switch name {
	case "String":
		return typecheck.TString(), true
	case "Integer":
		return typecheck.TInteger(), true
	case "Number":
		return typecheck.TNumber(), true
	case "Boolean":
		return typecheck.TBoolean(), true
	case "Null":
		return typecheck.TNull(), true
	case "Any":
		return typecheck.TOpaque(), true
	}
	return typecheck.Type{}, false
}

// genericTypeFromAST maps a generic instantiation like cfg.List[T].
// Only the cfg package's containers are known: List and Map take
// their element type from the type argument, and Object stands for
// the argument struct itself. Anything else stays Unknown.
func (w *walker) genericTypeFromAST(fn ast.Expr, arg ast.Expr) typecheck.Type {
	sel, ok := fn.(*ast.SelectorExpr)
	if !ok {
		return typecheck.TUnknown()
	}
	pkg, ok := identName(sel.X)
	if !ok || w.imports[pkg] != cfgPkgPath {
		return typecheck.TUnknown()
	}
	switch sel.Sel.Name {
	case "List":
		return typecheck.TList(w.typeFromAST(arg))
	case "Map":
		return typecheck.TMap(w.typeFromAST(arg))
	case "Object":
		return w.typeFromAST(arg)
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
		return typecheck.TOpaque(), true
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
	var out []typecheck.ObjectField
	w.eachStructField(st, func(kebab string, fld *ast.Field, _ bool) {
		out = append(out, objectField(kebab, w.typeFromAST(fld.Type)))
	})
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

func findModuleFunc(files []*ast.File) *ast.FuncDecl {
	return findPackageFunc(files, "Library")
}

func findPackageFunc(files []*ast.File, name string) *ast.FuncDecl {
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Recv != nil {
				continue
			}
			if fn.Name.Name == name {
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

// extractConfigurationRef finds the Configuration entry of the
// Library() return literal and returns the type and initializer behind
// its New function. found reports whether the library declares a
// Configuration at all; ok reports whether the struct type could be
// read from source, which requires the direct form
// `New: func() any { return &T{} }` (a `&pkg.T{}` works too).
type importedPackageLoader func(from *indexedPackage, alias string) (*indexedPackage, bool)

func extractConfigurationRef(
	fn *ast.FuncDecl,
	pkg *indexedPackage,
	load importedPackageLoader,
) (ref typeRef, init *ast.CompositeLit, found, ok bool) {
	if fn.Body == nil {
		return typeRef{}, nil, false, false
	}
	for _, stmt := range fn.Body.List {
		ret, retOk := stmt.(*ast.ReturnStmt)
		if !retOk || len(ret.Results) != 1 {
			continue
		}
		composite := unwrapModuleLiteral(ret.Results[0])
		if composite == nil {
			continue
		}
		for _, el := range composite.Elts {
			kv, kvOk := el.(*ast.KeyValueExpr)
			if !kvOk {
				continue
			}
			if name, nameOk := identName(kv.Key); !nameOk || name != "Configuration" {
				continue
			}
			ctLit := unwrapModuleLiteral(kv.Value)
			if ctLit == nil {
				return configurationCallRef(kv.Value, pkg, load)
			}
			return configurationTypeRef(ctLit)
		}
	}
	return typeRef{}, nil, false, false
}

func configurationCallRef(
	e ast.Expr,
	pkg *indexedPackage,
	load importedPackageLoader,
) (ref typeRef, init *ast.CompositeLit, found, ok bool) {
	call, callOk := e.(*ast.CallExpr)
	if !callOk || len(call.Args) != 0 {
		return typeRef{}, nil, true, false
	}
	if name, nameOk := identName(call.Fun); nameOk {
		if name != "LibraryConfiguration" {
			return typeRef{}, nil, true, false
		}
		fn := findPackageFunc(pkg.files, "LibraryConfiguration")
		if fn == nil {
			return typeRef{}, nil, true, false
		}
		return extractLibraryConfigurationRef(fn, pkg, load)
	}
	selector, selectorOk := call.Fun.(*ast.SelectorExpr)
	if !selectorOk || selector.Sel.Name != "LibraryConfiguration" {
		return typeRef{}, nil, true, false
	}
	alias, aliasOk := identName(selector.X)
	if !aliasOk || load == nil {
		return typeRef{}, nil, true, false
	}
	targetPkg, targetOk := load(pkg, alias)
	if !targetOk {
		return typeRef{}, nil, true, false
	}
	fn := findPackageFunc(targetPkg.files, "LibraryConfiguration")
	if fn == nil {
		return typeRef{}, nil, true, false
	}
	ref, init, found, ok = extractLibraryConfigurationRef(fn, targetPkg, load)
	return qualifyTypeRef(ref, targetPkg), init, found, ok
}

func extractLibraryConfigurationRef(
	fn *ast.FuncDecl,
	pkg *indexedPackage,
	load importedPackageLoader,
) (ref typeRef, init *ast.CompositeLit, found, ok bool) {
	if fn.Body == nil {
		return typeRef{}, nil, false, false
	}
	for _, stmt := range fn.Body.List {
		ret, retOk := stmt.(*ast.ReturnStmt)
		if !retOk || len(ret.Results) != 1 {
			continue
		}
		ctLit := unwrapModuleLiteral(ret.Results[0])
		if ctLit == nil {
			return configurationCallRef(ret.Results[0], pkg, load)
		}
		ref, init, found, ok = configurationTypeRef(ctLit)
		return qualifyTypeRef(ref, pkg), init, found, ok
	}
	return typeRef{}, nil, true, false
}

func qualifyTypeRef(ref typeRef, pkg *indexedPackage) typeRef {
	if ref.TypeName == "" || ref.ImportPath != "" || pkg == nil {
		return ref
	}
	if ref.PkgAlias != "" {
		if importPath := pkg.imports[ref.PkgAlias]; importPath != "" {
			ref.ImportPath = importPath
			ref.PkgAlias = ""
		}
		return ref
	}
	ref.ImportPath = pkg.importPath
	return ref
}

func configurationTypeRef(
	ctLit *ast.CompositeLit,
) (ref typeRef, init *ast.CompositeLit, found, ok bool) {
	for _, cel := range ctLit.Elts {
		ckv, ckvOk := cel.(*ast.KeyValueExpr)
		if !ckvOk {
			continue
		}
		if name, nameOk := identName(ckv.Key); !nameOk || name != "New" {
			continue
		}
		return configurationNewRef(ckv.Value)
	}
	return typeRef{}, nil, true, false
}

// configurationNewRef reads the struct type behind a ConfigurationType
// New field. Only a function literal whose single return is an
// address-of composite literal names the type in source.
func configurationNewRef(e ast.Expr) (ref typeRef, init *ast.CompositeLit, found, ok bool) {
	lit, litOk := e.(*ast.FuncLit)
	if !litOk || lit.Body == nil {
		return typeRef{}, nil, true, false
	}
	for _, stmt := range lit.Body.List {
		ret, retOk := stmt.(*ast.ReturnStmt)
		if !retOk || len(ret.Results) != 1 {
			continue
		}
		cl := unwrapModuleLiteral(ret.Results[0])
		if cl == nil {
			return typeRef{}, nil, true, false
		}
		r, refOk := outputTypeRef(cl.Type)
		return r, cl, true, refOk
	}
	return typeRef{}, nil, true, false
}

// extractFunctions returns each function in the Functions map of the
// Library() return literal keyed to its declared arity. The FunctionType
// values hold a Go func the dev CLI cannot run, but the name and the
// ArgCount/Variadic fields let the reference checker reject a call to an
// unknown function or one given the wrong number of arguments.
func extractFunctions(
	fn *ast.FuncDecl, files []*ast.File, errs *[]error,
) map[string]typecheck.FuncSig {
	out := map[string]typecheck.FuncSig{}
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
			if !ok || fieldName != "Functions" {
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
				if name, ok := stringLit(ekv.Key); ok {
					out[name] = functionSig(name, ekv.Value, files, errs)
				}
			}
		}
	}
	return out
}

// functionSig reads a function registration's signature. A MakeFunc
// call takes parameter and result types from the registered function's
// declaration; that is the only registration form, since anything else
// gives the checker a function it cannot type.
func functionSig(
	name string, e ast.Expr, files []*ast.File, errs *[]error,
) typecheck.FuncSig {
	if call, ok := e.(*ast.CallExpr); ok {
		return makeFuncSig(name, call, files, errs)
	}
	msg := fmt.Errorf("function %q: register with runtime.MakeFunc", name)
	if _, ok := e.(*ast.CompositeLit); ok {
		msg = fmt.Errorf(
			"function %q: register with runtime.MakeFunc; "+
				"a FunctionType literal declares no types", name)
	}
	if errs != nil {
		*errs = append(*errs, msg)
	}
	return typecheck.FuncSig{Result: typecheck.TUnknown()}
}

// makeFuncSig resolves a runtime.MakeFunc registration to its full
// signature by reading the registered function's declaration: the fn
// argument must name a function in the package or be a function
// literal. A registration outside that form, an unresolvable name,
// results other than (value, error), or an unsupported parameter type
// records an error, so a malformed registration fails the compile.
func makeFuncSig(
	name string, call *ast.CallExpr, files []*ast.File, errs *[]error,
) typecheck.FuncSig {
	addErr := func(format string, args ...any) {
		if errs != nil {
			*errs = append(*errs, fmt.Errorf(format, args...))
		}
	}
	if calleeName(call.Fun) != "MakeFunc" {
		addErr("function %q: register with runtime.MakeFunc", name)
		return typecheck.FuncSig{Result: typecheck.TUnknown()}
	}
	if len(call.Args) != 3 {
		addErr("function %q: MakeFunc takes a name, a description, and a function", name)
		return typecheck.FuncSig{Result: typecheck.TUnknown()}
	}
	var ft *ast.FuncType
	switch f := call.Args[2].(type) {
	case *ast.Ident:
		decl := findFuncDecl(files, f.Name)
		if decl == nil {
			addErr("function %q: MakeFunc references %s, which is not a function in the package",
				name, f.Name)
			return typecheck.FuncSig{Result: typecheck.TUnknown()}
		}
		ft = decl.Type
	case *ast.FuncLit:
		ft = f.Type
	default:
		addErr("function %q: the MakeFunc function must be a package function name"+
			" or a function literal", name)
		return typecheck.FuncSig{Result: typecheck.TUnknown()}
	}
	validateFuncDecl(name, ft, addErr)
	return funcTypeSig(ft)
}

// calleeName returns the bare name a call invokes, the selector's last
// segment for a qualified call.
func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// findFuncDecl returns the package-level function with the given name,
// or nil. Methods do not match.
func findFuncDecl(files []*ast.File, name string) *ast.FuncDecl {
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && fn.Name.Name == name {
				return fn
			}
		}
	}
	return nil
}

// validateFuncDecl records an error unless the declared signature
// returns (value, error) and every parameter is a type the evaluator's
// values convert to, mirroring what MakeFunc enforces at registration
// so the mistake fails the compile first.
func validateFuncDecl(name string, ft *ast.FuncType, addErr func(string, ...any)) {
	results := 0
	if ft.Results != nil {
		for _, f := range ft.Results.List {
			results += max(len(f.Names), 1)
		}
	}
	if results != 2 {
		word := "results"
		if results == 1 {
			word = "result"
		}
		addErr("function %q must return (value, error), got %d %s", name, results, word)
	} else {
		last := ft.Results.List[len(ft.Results.List)-1]
		if id, ok := last.Type.(*ast.Ident); !ok || id.Name != "error" {
			addErr("function %q must return (value, error)", name)
		}
		if !supportedParamType(ft.Results.List[0].Type) {
			addErr("function %q result has an unsupported type", name)
		}
	}
	if ft.Params == nil {
		return
	}
	pos := 0
	for _, fld := range ft.Params.List {
		typ := fld.Type
		if ell, ok := typ.(*ast.Ellipsis); ok {
			typ = ell.Elt
		}
		for range max(len(fld.Names), 1) {
			pos++
			if !supportedParamType(typ) {
				addErr("function %q parameter %d has an unsupported type", name, pos)
			}
		}
	}
}

// supportedParamType mirrors MakeFunc's parameter rule over the AST:
// bool, int64, float64, string, any, or a slice or string-keyed map of
// those.
func supportedParamType(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Ident:
		switch t.Name {
		case "bool", "int64", "float64", "string", "any":
			return true
		}
		return false
	case *ast.InterfaceType:
		return t.Methods == nil || len(t.Methods.List) == 0
	case *ast.ArrayType:
		return t.Len == nil && supportedParamType(t.Elt)
	case *ast.MapType:
		key, ok := t.Key.(*ast.Ident)
		return ok && key.Name == "string" && supportedParamType(t.Value)
	}
	return false
}

// funcTypeSig maps a declared signature onto the inferrer's view of
// it: one parameter type per declared name (a shared type spec counts
// each), the element type of a final ellipsis as the variadic tail,
// and the first result as the result type.
func funcTypeSig(ft *ast.FuncType) typecheck.FuncSig {
	var sig typecheck.FuncSig
	sig.Result = typecheck.TUnknown()
	if ft.Results != nil && len(ft.Results.List) > 0 {
		sig.Result = astValueType(ft.Results.List[0].Type)
	}
	if ft.Params == nil {
		return sig
	}
	for _, fld := range ft.Params.List {
		if ell, ok := fld.Type.(*ast.Ellipsis); ok {
			elem := astValueType(ell.Elt)
			sig.Variadic = &elem
			continue
		}
		t := astValueType(fld.Type)
		for range max(len(fld.Names), 1) {
			sig.Params = append(sig.Params, t)
		}
	}
	return sig
}

// astValueType maps a declared parameter or result type onto the
// language type it converts to and from. A spelling outside the
// supported set reads as Unknown, which checks nothing.
func astValueType(e ast.Expr) typecheck.Type {
	switch t := e.(type) {
	case *ast.Ident:
		switch t.Name {
		case "bool":
			return typecheck.TBoolean()
		case "int64":
			return typecheck.TInteger()
		case "float64":
			return typecheck.TNumber()
		case "string":
			return typecheck.TString()
		case "any":
			return typecheck.TOpaque()
		}
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return typecheck.TOpaque()
		}
	case *ast.ArrayType:
		if t.Len == nil {
			return typecheck.TList(astValueType(t.Elt))
		}
	case *ast.MapType:
		if key, ok := t.Key.(*ast.Ident); ok && key.Name == "string" {
			return typecheck.TMap(astValueType(t.Value))
		}
	}
	return typecheck.TUnknown()
}

// refsFromMakeCall extracts the input and output type references
// from a `runtime.MakeResource[T, Out, any](...)` call. It accepts the
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
// call's function part, in source order. The call `MakeResource[T, Out, any]()`
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

// parseUBFieldTag reads a field's `ub` struct tag from its source
// form, through the same parser the codec uses at run time. name is
// empty when the field falls back to its kebab-cased Go name. unknown
// options are returned for warning, so a typo like "sensitiv" cannot
// quietly leave a secret unmasked.
func parseUBFieldTag(tag *ast.BasicLit) (name string, skip, sensitive bool, unknown []string) {
	if tag == nil {
		return "", false, false, nil
	}
	t := ub.ParseTag(reflect.StructTag(strings.Trim(tag.Value, "`")).Get("ub"))
	return t.Name, t.Skip, t.Sensitive, t.Unknown
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

// boolLit reads a true or false literal.
func boolLit(e ast.Expr) (bool, bool) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false, false
	}
	switch id.Name {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

func unquoteString(s string) (string, error) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1], nil
	}
	return "", fmt.Errorf("not a double-quoted string: %s", s)
}

func packageModuleRoot(dir string) (ModuleRoot, string) {
	moduleDir, modulePath, ok := findGoModule(dir)
	if !ok {
		return ModuleRoot{Dir: dir}, ""
	}
	importPath := modulePath
	if rel, err := filepath.Rel(moduleDir, dir); err == nil && rel != "." {
		importPath = pathpkg.Join(modulePath, filepath.ToSlash(rel))
	}
	return ModuleRoot{Path: modulePath, Dir: moduleDir}, importPath
}

func findGoModule(dir string) (moduleDir, modulePath string, ok bool) {
	for cur := filepath.Clean(dir); ; cur = filepath.Dir(cur) {
		modulePath := readGoModPath(cur)
		if modulePath != "" {
			return cur, modulePath, true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", "", false
		}
	}
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
