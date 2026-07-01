package goschema

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// GoLocation points at a byte in a Go source file.
type GoLocation struct {
	Path   string
	Line   int
	Column int
	Offset int
}

// SourceIndex records source locations for a Go library schema.
type SourceIndex struct {
	LibraryFunc   GoLocation
	Registrations map[string]map[string]GoLocation
	InputTypes    map[string]map[string]GoLocation
	OutputTypes   map[string]map[string]GoLocation
	InputFields   map[string]map[string]map[string]GoLocation
	OutputFields  map[string]map[string]map[string]GoLocation
	ConfigType    GoLocation
	ConfigFields  map[string]GoLocation
	Functions     map[string]GoLocation
}

// ReadWithIndex reads a Go library schema and the matching source index.
func ReadWithIndex(dir string, extra ...ModuleRoot) (
	schema *runtime.LibrarySchema,
	index *SourceIndex,
	warnings []string,
	err error,
) {
	analysis, err := Analyze(dir, extra...)
	if err != nil {
		if analysis == nil {
			return nil, nil, nil, err
		}
		return analysis.Schema, nil, analysis.Warnings, err
	}
	return analysis.Schema, analysis.Index, analysis.Warnings, nil
}

// ReadLibraryConfigurationWithIndex reads a config-schema package and source index.
func ReadLibraryConfigurationWithIndex(dir string, extra ...ModuleRoot) (
	schema *runtime.LibrarySchema,
	index *SourceIndex,
	warnings []string,
	err error,
) {
	ctx, err := newAnalysisContext(dir, extra...)
	if err != nil {
		return nil, nil, nil, err
	}
	fn := findPackageFunc(ctx.root.files, "LibraryConfiguration")
	if fn == nil {
		return nil, nil, nil, fmt.Errorf("no LibraryConfiguration() function in %s", dir)
	}
	schema, err = ctx.readLibraryConfigurationSchema(fn)
	if err != nil {
		return schema, nil, ctx.warnings, err
	}
	index, err = ctx.buildLibraryConfigurationSourceIndex(fn)
	if err != nil {
		return schema, nil, ctx.warnings, err
	}
	return schema, index, ctx.warnings, nil
}

// SourceIndexCache caches schema and source-index reads by directory.
type SourceIndexCache struct {
	extra   []ModuleRoot
	entries map[string]sourceIndexCacheEntry
}

type sourceIndexCacheEntry struct {
	schema                *runtime.LibrarySchema
	index                 *SourceIndex
	warnings              []string
	configurationSchema   *runtime.LibrarySchema
	configurationIndex    *SourceIndex
	configurationWarnings []string
}

// NewSourceIndexCache returns an empty source index cache.
func NewSourceIndexCache(extra ...ModuleRoot) *SourceIndexCache {
	return &SourceIndexCache{
		extra:   append([]ModuleRoot(nil), extra...),
		entries: map[string]sourceIndexCacheEntry{},
	}
}

// Read reads a schema and source index, using the cache when present.
func (c *SourceIndexCache) Read(dir string) (
	schema *runtime.LibrarySchema,
	index *SourceIndex,
	warnings []string,
	err error,
) {
	if c == nil {
		return ReadWithIndex(dir)
	}
	key := filepath.Clean(dir)
	if entry, ok := c.entries[key]; ok && entry.schema != nil {
		return entry.schema, entry.index, entry.warnings, nil
	}
	schema, index, warnings, err = ReadWithIndex(dir, c.extra...)
	if err != nil {
		return schema, index, warnings, err
	}
	entry := c.entries[key]
	entry.schema = schema
	entry.index = index
	entry.warnings = append([]string(nil), warnings...)
	c.entries[key] = entry
	return schema, index, warnings, nil
}

// ReadLibraryConfiguration reads a config-schema package and source index.
func (c *SourceIndexCache) ReadLibraryConfiguration(dir string) (
	schema *runtime.LibrarySchema,
	index *SourceIndex,
	warnings []string,
	err error,
) {
	if c == nil {
		return ReadLibraryConfigurationWithIndex(dir)
	}
	key := filepath.Clean(dir)
	if entry, ok := c.entries[key]; ok && entry.configurationSchema != nil {
		return entry.configurationSchema, entry.configurationIndex,
			entry.configurationWarnings, nil
	}
	schema, index, warnings, err = ReadLibraryConfigurationWithIndex(dir, c.extra...)
	if err != nil {
		return schema, index, warnings, err
	}
	entry := c.entries[key]
	entry.configurationSchema = schema
	entry.configurationIndex = index
	entry.configurationWarnings = append([]string(nil), warnings...)
	c.entries[key] = entry
	return schema, index, warnings, nil
}

// Invalidate removes one directory from the cache.
func (c *SourceIndexCache) Invalidate(dir string) {
	if c == nil {
		return
	}
	delete(c.entries, filepath.Clean(dir))
}

func (i *sourceIndexer) build(libraryFunc *ast.FuncDecl) (*SourceIndex, error) {
	rootPkg := i.root
	index := newSourceIndex()
	index.LibraryFunc = rootPkg.location(libraryFunc.Name.Pos())
	for _, site := range extractRegistrationSites(libraryFunc) {
		kind, ok := sourceIndexKind(site.Field)
		if !ok {
			continue
		}
		index.Registrations[kind][site.Name] = rootPkg.location(site.NamePos)
		if typePkg, spec, ok := i.resolveType(rootPkg, site.InputRef); ok {
			index.InputTypes[kind][site.Name] = typePkg.location(spec.Name.Pos())
			index.InputFields[kind][site.Name] = i.fieldLocations(typePkg, spec.Name.Name)
		}
		if typePkg, spec, ok := i.resolveType(rootPkg, site.OutputRef); ok {
			index.OutputTypes[kind][site.Name] = typePkg.location(spec.Name.Pos())
			index.OutputFields[kind][site.Name] = i.fieldLocations(typePkg, spec.Name.Name)
		}
	}
	if ref, _, found, ok := extractConfigurationRef(
		libraryFunc,
		rootPkg,
		i.loadImportedPackage,
	); found && ok {
		if typePkg, spec, ok := i.resolveType(rootPkg, ref); ok {
			index.ConfigType = typePkg.location(spec.Name.Pos())
			index.ConfigFields = i.fieldLocations(typePkg, spec.Name.Name)
		}
	}
	index.Functions = extractFunctionLocations(rootPkg, libraryFunc)
	return index, nil
}

func (i *sourceIndexer) buildLibraryConfiguration(fn *ast.FuncDecl) (*SourceIndex, error) {
	index := newSourceIndex()
	ref, _, found, ok := extractLibraryConfigurationRef(
		fn,
		i.root,
		i.loadImportedPackage,
	)
	if found && ok {
		if typePkg, spec, ok := i.resolveType(i.root, ref); ok {
			index.ConfigType = typePkg.location(spec.Name.Pos())
			index.ConfigFields = i.fieldLocations(typePkg, spec.Name.Name)
		}
	}
	return index, nil
}

func newSourceIndex() *SourceIndex {
	return &SourceIndex{
		Registrations: newKindLocationMap(),
		InputTypes:    newKindLocationMap(),
		OutputTypes:   newKindLocationMap(),
		InputFields:   newKindFieldMap(),
		OutputFields:  newKindFieldMap(),
		ConfigFields:  map[string]GoLocation{},
		Functions:     map[string]GoLocation{},
	}
}

func newKindLocationMap() map[string]map[string]GoLocation {
	return map[string]map[string]GoLocation{
		"resource":    {},
		"data-source": {},
		"action":      {},
	}
}

func newKindFieldMap() map[string]map[string]map[string]GoLocation {
	return map[string]map[string]map[string]GoLocation{
		"resource":    {},
		"data-source": {},
		"action":      {},
	}
}

type indexedPackage struct {
	fset       *token.FileSet
	files      []*ast.File
	dir        string
	importPath string
	imports    map[string]string
}

func (p *indexedPackage) location(pos token.Pos) GoLocation {
	if p == nil || !pos.IsValid() {
		return GoLocation{}
	}
	position := p.fset.Position(pos)
	return GoLocation{
		Path:   position.Filename,
		Line:   position.Line,
		Column: position.Column,
		Offset: position.Offset,
	}
}

type sourceIndexer struct {
	roots    []ModuleRoot
	packages map[string]*indexedPackage
	root     *indexedPackage
}

func (i *sourceIndexer) loadImportedPackage(
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
	return i.loadPackage(importPath)
}

func (i *sourceIndexer) loadPackage(importPath string) (*indexedPackage, bool) {
	if pkg, ok := i.packages[importPath]; ok {
		return pkg, true
	}
	root, ok := i.rootFor(importPath)
	if !ok {
		return nil, false
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(importPath, root.Path), "/")
	pkg, err := parseIndexedPackageDir(filepath.Join(root.Dir, rel), importPath)
	if err != nil {
		return nil, false
	}
	i.packages[importPath] = pkg
	return pkg, true
}

func (i *sourceIndexer) rootFor(importPath string) (ModuleRoot, bool) {
	var best ModuleRoot
	found := false
	for _, root := range i.roots {
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

func (i *sourceIndexer) resolveType(
	pkg *indexedPackage,
	ref typeRef,
) (*indexedPackage, *ast.TypeSpec, bool) {
	if ref.TypeName == "" {
		return nil, nil, false
	}
	typePkg := pkg
	importPath := ref.ImportPath
	if ref.PkgAlias != "" {
		var ok bool
		importPath, ok = pkg.imports[ref.PkgAlias]
		if !ok {
			return nil, nil, false
		}
	}
	if importPath != "" && importPath != pkg.importPath {
		var found bool
		typePkg, found = i.loadPackage(importPath)
		if !found {
			return nil, nil, false
		}
	}
	spec := findTypeSpec(typePkg.files, ref.TypeName)
	if spec == nil {
		return nil, nil, false
	}
	return typePkg, spec, true
}

func (i *sourceIndexer) fieldLocations(
	pkg *indexedPackage,
	typeName string,
) map[string]GoLocation {
	out := map[string]GoLocation{}
	i.collectNamedFields(pkg, typeName, "", out, map[string]bool{})
	return out
}

func (i *sourceIndexer) collectNamedFields(
	pkg *indexedPackage,
	typeName string,
	prefix string,
	out map[string]GoLocation,
	visiting map[string]bool,
) {
	if pkg == nil || typeName == "" {
		return
	}
	key := pkg.importPath + "." + typeName
	if visiting[key] {
		return
	}
	spec := findTypeSpec(pkg.files, typeName)
	if spec == nil {
		return
	}
	visiting[key] = true
	defer delete(visiting, key)
	i.collectFieldsFromExpr(pkg, spec.Type, prefix, out, visiting)
}

func (i *sourceIndexer) collectFieldsFromExpr(
	pkg *indexedPackage,
	expr ast.Expr,
	prefix string,
	out map[string]GoLocation,
	visiting map[string]bool,
) {
	switch t := expr.(type) {
	case *ast.StarExpr:
		i.collectFieldsFromExpr(pkg, t.X, prefix, out, visiting)
	case *ast.ArrayType:
		i.collectFieldsFromExpr(pkg, t.Elt, prefix, out, visiting)
	case *ast.MapType:
		i.collectFieldsFromExpr(pkg, t.Value, prefix, out, visiting)
	case *ast.IndexExpr:
		i.collectFieldsFromExpr(pkg, t.Index, prefix, out, visiting)
	case *ast.Ident:
		if _, ok := primitiveFromName(t.Name); ok || t.Name == "error" {
			return
		}
		i.collectNamedFields(pkg, t.Name, prefix, out, visiting)
	case *ast.SelectorExpr:
		alias, ok := identName(t.X)
		if !ok {
			return
		}
		importPath, ok := pkg.imports[alias]
		if !ok {
			return
		}
		sub, ok := i.loadPackage(importPath)
		if !ok {
			return
		}
		i.collectNamedFields(sub, t.Sel.Name, prefix, out, visiting)
	case *ast.StructType:
		i.collectStructFields(pkg, t, prefix, out, visiting)
	}
}

func (i *sourceIndexer) collectStructFields(
	pkg *indexedPackage,
	st *ast.StructType,
	prefix string,
	out map[string]GoLocation,
	visiting map[string]bool,
) {
	if st.Fields == nil {
		return
	}
	for _, field := range st.Fields.List {
		name, skip, _, _ := parseUBFieldTag(field.Tag)
		if skip || len(field.Names) == 0 {
			continue
		}
		for _, fieldName := range field.Names {
			kebab := name
			if kebab == "" {
				kebab = lang.PascalToKebab(fieldName.Name)
			}
			path := prefix + kebab
			out[path] = pkg.location(fieldName.Pos())
			i.collectFieldsFromExpr(pkg, field.Type, path+".", out, visiting)
		}
	}
}

type registrationSite struct {
	registration
	NamePos token.Pos
}

func extractRegistrationSites(fn *ast.FuncDecl) []registrationSite {
	var out []registrationSite
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
		out = append(out, registrationSitesFromLiteral(composite)...)
	}
	return out
}

func registrationSitesFromLiteral(composite *ast.CompositeLit) []registrationSite {
	var out []registrationSite
	for _, el := range composite.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		fieldName, ok := identName(kv.Key)
		if !ok {
			continue
		}
		if _, ok := sourceIndexKind(fieldName); !ok {
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
			out = append(out, registrationSite{
				registration: registration{
					Field:     fieldName,
					Name:      kebab,
					InputRef:  inputRef,
					OutputRef: outputRef,
				},
				NamePos: ekv.Key.Pos(),
			})
		}
	}
	return out
}

func extractFunctionLocations(pkg *indexedPackage, fn *ast.FuncDecl) map[string]GoLocation {
	out := map[string]GoLocation{}
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
			if !ok || calleeName(kv.Key) != "Functions" {
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
				name, ok := stringLit(ekv.Key)
				if !ok {
					continue
				}
				if loc, ok := functionLocation(pkg, ekv.Value); ok {
					out[name] = loc
				}
			}
		}
	}
	return out
}

func functionLocation(pkg *indexedPackage, expr ast.Expr) (GoLocation, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || calleeName(call.Fun) != "MakeFunc" || len(call.Args) != 3 {
		return GoLocation{}, false
	}
	switch fn := call.Args[2].(type) {
	case *ast.Ident:
		decl := findFuncDecl(pkg.files, fn.Name)
		if decl == nil {
			return GoLocation{}, false
		}
		return pkg.location(decl.Name.Pos()), true
	case *ast.FuncLit:
		return pkg.location(fn.Type.Func), true
	default:
		return GoLocation{}, false
	}
}

func sourceIndexKind(field string) (string, bool) {
	switch field {
	case "Resources":
		return "resource", true
	case "DataSources":
		return "data-source", true
	case "Actions":
		return "action", true
	default:
		return "", false
	}
}

func parseIndexedPackageDir(dir string, importPath string) (*indexedPackage, error) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", dir, err)
	}
	var packageName string
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", dir, err)
		}
		if packageName == "" {
			packageName = file.Name.Name
		}
		if file.Name.Name != packageName {
			return nil, fmt.Errorf("more than one Go package found in %s", dir)
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Go package found in %s", dir)
	}
	return &indexedPackage{
		fset:       fset,
		files:      files,
		dir:        dir,
		importPath: importPath,
		imports:    buildImportMap(files),
	}, nil
}
