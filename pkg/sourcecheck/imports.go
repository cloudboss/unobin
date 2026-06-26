package sourcecheck

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"

	"github.com/cloudboss/unobin/pkg/codegen"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// ImportAnalysis is the resolved import data shared by source checks,
// compile, and graph printing.
type ImportAnalysis struct {
	Top        []resolve.Resolution
	Libraries  map[string]*runtime.Library
	GoImports  map[string]string
	GoModules  map[string]string
	UBImports  map[string]string
	UBPackages map[string][]byte
}

// ImportAnalysisOptions configures AnalyzeImports.
type ImportAnalysisOptions struct {
	ProjectDir              string
	Source                  *resolve.Source
	Resolver                resolve.Resolver
	Versions                map[string]string
	SchemaCache             *SchemaCache
	WarnOut                 io.Writer
	Mode                    Mode
	StackName               string
	GeneratePackages        bool
	ValidateCompositeBodies bool
}

// AnalyzeImports resolves refs once and builds the data each caller needs.
func AnalyzeImports(
	refs map[string]resolve.ImportRef,
	opts ImportAnalysisOptions,
) (*ImportAnalysis, error) {
	if len(refs) > 0 && opts.Resolver == nil {
		return nil, errors.New("sourcecheck: resolver is required when imports are present")
	}
	resolver := opts.Resolver
	if opts.Mode == ModeNoFetch {
		resolver = noFetchResolver{wrapped: resolver}
	}
	schemas := opts.SchemaCache
	if schemas == nil {
		schemas = NewSchemaCache()
	}
	visitor := newImportVisitor(opts, schemas)
	top, err := resolve.WalkUBFrom(refs, resolver, visitor, opts.Versions,
		importSourceForOptions(opts))
	if err != nil {
		return nil, err
	}
	analysis := &ImportAnalysis{
		Top:        top,
		Libraries:  make(map[string]*runtime.Library, len(top)),
		GoImports:  map[string]string{},
		GoModules:  visitor.goModules,
		UBImports:  map[string]string{},
		UBPackages: visitor.packages,
	}
	for _, res := range top {
		switch res.Kind {
		case resolve.ResolutionGo:
			schema, warnings, err := schemas.Read(res.SourcePath)
			if err != nil {
				return nil, fmt.Errorf("import %q: %w", res.LocalAlias, err)
			}
			printSchemaWarnings(opts.WarnOut, res.LocalAlias, warnings)
			analysis.GoImports[res.LocalAlias] = res.Path
			analysis.Libraries[res.LocalAlias] = &runtime.Library{Schema: schema}
		case resolve.ResolutionUB:
			analysis.Libraries[res.LocalAlias] = visitor.runtimeLibraries[res.CanonicalKey]
			if opts.GeneratePackages {
				importPath, err := visitor.ubImportPath(res.CanonicalKey)
				if err != nil {
					return nil, err
				}
				analysis.UBImports[res.LocalAlias] = importPath
			}
		}
	}
	return analysis, nil
}

func importSourceForOptions(opts ImportAnalysisOptions) *resolve.Source {
	if opts.Source != nil {
		return opts.Source
	}
	if opts.ProjectDir == "" {
		return nil
	}
	return &resolve.Source{FS: os.DirFS(opts.ProjectDir), Path: opts.ProjectDir}
}

type importVisitor struct {
	stackName               string
	generatePackages        bool
	validateCompositeBodies bool
	packageIDs              *ubPackageIDs
	packageIDByKey          map[string]string
	packages                map[string][]byte
	goModules               map[string]string
	runtimeLibraries        map[string]*runtime.Library
	warnOut                 io.Writer
	schemas                 *SchemaCache
}

func newImportVisitor(opts ImportAnalysisOptions, schemas *SchemaCache) *importVisitor {
	stackName := opts.StackName
	if stackName == "" {
		stackName = "stack"
	}
	packages := map[string][]byte{}
	var ids *ubPackageIDs
	var packageIDByKey map[string]string
	if opts.GeneratePackages {
		ids = newUBPackageIDs()
		packageIDByKey = ids.byKey
	}
	return &importVisitor{
		stackName:               stackName,
		generatePackages:        opts.GeneratePackages,
		validateCompositeBodies: opts.ValidateCompositeBodies,
		packageIDs:              ids,
		packageIDByKey:          packageIDByKey,
		packages:                packages,
		goModules:               map[string]string{},
		runtimeLibraries:        map[string]*runtime.Library{},
		warnOut:                 opts.WarnOut,
		schemas:                 schemas,
	}
}

func (v *importVisitor) OnGoImport(_, _, modulePath, version string) error {
	if deps.IsReplacementSentinel(version) {
		goVersion, err := deps.GoReplacementSentinel(modulePath)
		if err != nil {
			return err
		}
		version = goVersion
	}
	v.goModules[modulePath] = version
	return nil
}

func (v *importVisitor) ubImportPath(canonicalKey string) (string, error) {
	packageID, ok := v.packageIDByKey[canonicalKey]
	if !ok {
		return "", fmt.Errorf("compile: missing generated package ID for %s", canonicalKey)
	}
	return v.stackName + "/internal/" + packageID, nil
}

func (v *importVisitor) OnUBLibrary(
	alias, canonicalKey string, _ resolve.ImportRef, lib *resolve.UBLibrary,
) error {
	entries := lib.CompositeEntries()
	if v.validateCompositeBodies {
		var violations []error
		for _, entry := range entries {
			violations = append(violations,
				resolve.ValidateSyntaxCompositeBody(entry.Kind, entry.Name, entry.SyntaxBody)...)
		}
		if len(violations) > 0 {
			return errors.Join(violations...)
		}
	}

	packageID := ""
	if v.generatePackages {
		packageID = v.packageIDs.ID(alias, canonicalKey)
	}
	composites, err := v.buildCompiledComposites(entries, lib.BodyImports)
	if err != nil {
		return err
	}
	runtimeLib := runtimeLibraryForCompiledComposites(alias, composites)
	if v.generatePackages {
		src, err := codegen.GenerateUBLibraryPackage(
			packageID,
			alias,
			syntaxBodiesForCompiledComposites(composites),
			codegenImportsForCompiledComposites(composites),
			goSpecsForCompiledComposites(composites),
		)
		if err != nil {
			return err
		}
		v.packages[packageID] = src
	}
	v.runtimeLibraries[canonicalKey] = runtimeLib
	return nil
}

type compiledComposite struct {
	entry          resolve.CompositeEntry
	bodyLibs       map[string]*runtime.Library
	codegenImports map[string]string
	goSpecs        map[string]codegen.GoLibrarySpecs
}

func (v *importVisitor) buildCompiledComposites(
	entries []resolve.CompositeEntry,
	bodyImports map[string]map[string][]resolve.Resolution,
) ([]compiledComposite, error) {
	composites := make([]compiledComposite, 0, len(entries))
	for _, entry := range entries {
		resols := bodyImports[entry.Kind][entry.Name]
		composite := compiledComposite{
			entry:    entry,
			bodyLibs: make(map[string]*runtime.Library, len(resols)),
		}
		if v.generatePackages {
			composite.codegenImports = make(map[string]string, len(resols))
			composite.goSpecs = map[string]codegen.GoLibrarySpecs{}
		}
		bodyUsed := usedSyntaxLibraryTypes(entry.SyntaxBody)
		for _, res := range resols {
			switch res.Kind {
			case resolve.ResolutionGo:
				if err := v.addCompiledGoImport(&composite, entry, bodyUsed, res); err != nil {
					return nil, err
				}
			case resolve.ResolutionUB:
				if err := v.addCompiledUBImport(&composite, res); err != nil {
					return nil, err
				}
			}
		}
		if len(composite.codegenImports) == 0 {
			composite.codegenImports = nil
		}
		if len(composite.goSpecs) == 0 {
			composite.goSpecs = nil
		}
		composites = append(composites, composite)
	}
	return composites, nil
}

func (v *importVisitor) addCompiledGoImport(
	composite *compiledComposite,
	entry resolve.CompositeEntry,
	bodyUsed map[string]map[string]bool,
	res resolve.Resolution,
) error {
	schema, warnings, err := v.schemas.Read(res.SourcePath)
	if err != nil {
		return fmt.Errorf(
			"%s composite %q import %q: %w",
			entry.Kind, entry.Name, res.LocalAlias, err)
	}
	printSchemaWarnings(v.warnOut, res.LocalAlias, warnings)
	composite.bodyLibs[res.LocalAlias] = &runtime.Library{Schema: schema}
	if !v.generatePackages {
		return nil
	}
	composite.codegenImports[res.LocalAlias] = res.Path
	used := bodyUsed[res.LocalAlias]
	specs := codegen.GoLibrarySpecs{
		Constraints: keepUsedTypes(constraintsFromSchema(schema), used),
		Defaults:    keepUsedTypes(defaultsFromSchema(schema), used),
		Schema:      keepUsedSchema(schema, used),
	}
	if !specs.Empty() {
		composite.goSpecs[res.Path] = specs
	}
	return nil
}

func (v *importVisitor) addCompiledUBImport(
	composite *compiledComposite,
	res resolve.Resolution,
) error {
	composite.bodyLibs[res.LocalAlias] = v.runtimeLibraries[res.CanonicalKey]
	if !v.generatePackages {
		return nil
	}
	importPath, err := v.ubImportPath(res.CanonicalKey)
	if err != nil {
		return err
	}
	composite.codegenImports[res.LocalAlias] = importPath
	return nil
}

func runtimeLibraryForCompiledComposites(
	name string,
	composites []compiledComposite,
) *runtime.Library {
	lib := &runtime.Library{Name: name}
	for _, composite := range composites {
		syntaxBody := composite.entry.SyntaxBody
		lib.AddComposite(&runtime.CompositeType{
			Name:       composite.entry.Name,
			Kind:       runtime.NodeKind(composite.entry.Kind),
			SyntaxBody: &syntaxBody,
			Libraries:  composite.bodyLibs,
		})
	}
	return lib
}

func syntaxBodiesForCompiledComposites(
	composites []compiledComposite,
) map[string]map[string]syntax.FactoryBody {
	out := map[string]map[string]syntax.FactoryBody{}
	for _, composite := range composites {
		kind := composite.entry.Kind
		if out[kind] == nil {
			out[kind] = map[string]syntax.FactoryBody{}
		}
		out[kind][composite.entry.Name] = composite.entry.SyntaxBody
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func codegenImportsForCompiledComposites(
	composites []compiledComposite,
) map[string]map[string]map[string]string {
	out := map[string]map[string]map[string]string{}
	for _, composite := range composites {
		if len(composite.codegenImports) == 0 {
			continue
		}
		kind := composite.entry.Kind
		if out[kind] == nil {
			out[kind] = map[string]map[string]string{}
		}
		imports := make(map[string]string, len(composite.codegenImports))
		maps.Copy(imports, composite.codegenImports)
		out[kind][composite.entry.Name] = imports
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func goSpecsForCompiledComposites(
	composites []compiledComposite,
) map[string]codegen.GoLibrarySpecs {
	out := map[string]codegen.GoLibrarySpecs{}
	for _, composite := range composites {
		for importPath, specs := range composite.goSpecs {
			mergeGoLibrarySpecs(out, importPath, specs)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeGoLibrarySpecs(
	out map[string]codegen.GoLibrarySpecs,
	importPath string,
	specs codegen.GoLibrarySpecs,
) {
	if specs.Empty() {
		return
	}
	current := out[importPath]
	current.Constraints = mergeSpecMap(current.Constraints, specs.Constraints)
	current.Defaults = mergeSpecMap(current.Defaults, specs.Defaults)
	current.Schema = mergeLibrarySchema(current.Schema, specs.Schema)
	out[importPath] = current
}

func mergeSpecMap[T any](dst, src map[string][]T) map[string][]T {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string][]T{}
	}
	maps.Copy(dst, src)
	return dst
}

func mergeLibrarySchema(
	dst *runtime.LibrarySchema,
	src *runtime.LibrarySchema,
) *runtime.LibrarySchema {
	if src == nil {
		return dst
	}
	if dst == nil {
		dst = &runtime.LibrarySchema{}
	}
	dst.Resources = mergeTypeSchemaMap(dst.Resources, src.Resources)
	dst.DataSources = mergeTypeSchemaMap(dst.DataSources, src.DataSources)
	dst.Actions = mergeTypeSchemaMap(dst.Actions, src.Actions)
	return dst
}

func mergeTypeSchemaMap(
	dst map[string]*runtime.TypeSchema,
	src map[string]*runtime.TypeSchema,
) map[string]*runtime.TypeSchema {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string]*runtime.TypeSchema{}
	}
	maps.Copy(dst, src)
	return dst
}

func constraintsFromSchema(schema *runtime.LibrarySchema) map[string][]lang.ConstraintSpec {
	return typeSpecsFromSchema(schema, func(ts *runtime.TypeSchema) []lang.ConstraintSpec {
		return ts.Constraints
	})
}

func defaultsFromSchema(schema *runtime.LibrarySchema) map[string][]lang.DefaultSpec {
	return typeSpecsFromSchema(schema, func(ts *runtime.TypeSchema) []lang.DefaultSpec {
		return ts.Defaults
	})
}

func typeSpecsFromSchema[T any](
	schema *runtime.LibrarySchema,
	pick func(*runtime.TypeSchema) []T,
) map[string][]T {
	if schema == nil {
		return nil
	}
	out := map[string][]T{}
	add := func(kind runtime.NodeKind, types map[string]*runtime.TypeSchema) {
		for typ, ts := range types {
			if specs := pick(ts); len(specs) > 0 {
				out[string(kind)+"."+typ] = specs
			}
		}
	}
	add(runtime.NodeResource, schema.Resources)
	add(runtime.NodeDataSource, schema.DataSources)
	add(runtime.NodeAction, schema.Actions)
	if len(out) == 0 {
		return nil
	}
	return out
}

func usedSyntaxLibraryTypes(body syntax.FactoryBody) map[string]map[string]bool {
	used := map[string]map[string]bool{}
	add := func(kind string, decls []syntax.NodeDecl) {
		for _, decl := range decls {
			addUsedLibraryType(
				used,
				decl.Selector.Alias.Name,
				kind,
				decl.Selector.Export.Name,
			)
		}
	}
	add("resource", body.Resources)
	add(string(runtime.NodeDataSource), body.Data)
	add("action", body.Actions)
	return used
}

func addUsedLibraryType(used map[string]map[string]bool, alias, kind, export string) {
	if used[alias] == nil {
		used[alias] = map[string]bool{}
	}
	used[alias][kind+"."+export] = true
}

func keepUsedTypes[T any](m map[string][]T, used map[string]bool) map[string][]T {
	out := map[string][]T{}
	for key, specs := range m {
		if used[key] {
			out[key] = specs
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func keepUsedSchema(
	schema *runtime.LibrarySchema,
	used map[string]bool,
) *runtime.LibrarySchema {
	if schema == nil {
		return nil
	}
	out := &runtime.LibrarySchema{
		Resources:   keepSensitiveTypes(schema.Resources, used, string(runtime.NodeResource)),
		DataSources: keepSensitiveTypes(schema.DataSources, used, string(runtime.NodeDataSource)),
		Actions:     keepSensitiveTypes(schema.Actions, used, string(runtime.NodeAction)),
	}
	if len(out.Resources)+len(out.DataSources)+len(out.Actions) == 0 {
		return nil
	}
	return out
}

func keepSensitiveTypes(
	types map[string]*runtime.TypeSchema,
	used map[string]bool,
	kind string,
) map[string]*runtime.TypeSchema {
	out := map[string]*runtime.TypeSchema{}
	for typ, ts := range types {
		if !used[kind+"."+typ] || !typeHasSensitivity(ts) {
			continue
		}
		out[typ] = &runtime.TypeSchema{
			SensitiveInputs:  append([]string(nil), ts.SensitiveInputs...),
			SensitiveOutputs: append([]string(nil), ts.SensitiveOutputs...),
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func typeHasSensitivity(ts *runtime.TypeSchema) bool {
	return ts != nil && (len(ts.SensitiveInputs) > 0 || len(ts.SensitiveOutputs) > 0)
}
