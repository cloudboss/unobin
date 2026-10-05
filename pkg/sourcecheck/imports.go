package sourcecheck

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/codegen"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// ImportAnalysis is the resolved import data shared by source checks,
// compile, and graph printing.
type ImportAnalysis struct {
	Top                  []resolve.Resolution
	Libraries            map[string]*runtime.Library
	LibraryConfigSchemas map[string]runtime.LibraryConfigSchema
	GoImports            map[string]string
	GoModules            map[string]string
	UBImports            map[string]string
	UBPackages           map[string][]byte
	Assets               *asset.Collection
	RootAssetSetID       string
	Compatibility        *golibrary.CompatibilityContext
	LibraryMetadata      []golibrary.PackageMetadata
}

// ImportAnalysisOptions configures AnalyzeImports.
type ImportAnalysisOptions struct {
	ProjectDir              string
	Source                  *resolve.Source
	Resolver                resolve.Resolver
	Versions                map[string]string
	SchemaCache             *SchemaCache
	Reporter                diagnostic.Reporter
	Mode                    Mode
	StackName               string
	GeneratePackages        bool
	ValidateCompositeBodies bool
	Body                    *syntax.FactoryBody
	RootSourceFile          syntax.SourceFileSpec
	GeneratedOutputPath     string
	Compatibility           *golibrary.CompatibilityContext
}

func AnalyzeImports(
	refs map[string]resolve.ImportRef,
	opts ImportAnalysisOptions,
) (*ImportAnalysis, error) {
	checked, err := AnalyzeProgram(refs, opts)
	if err != nil {
		return nil, err
	}
	generated := &codegen.GeneratedImports{
		GoImports: map[string]string{}, UBImports: map[string]string{},
		UBPackages: map[string][]byte{},
	}
	if opts.GeneratePackages {
		generated, err = codegen.GenerateImports(checked, opts.StackName)
		if err != nil {
			return nil, err
		}
	} else {
		for _, imported := range checked.Top {
			if imported.Kind == resolve.ResolutionGo {
				generated.GoImports[imported.LocalAlias] = imported.Path
			}
		}
	}
	return &ImportAnalysis{
		Top: checked.Top, Libraries: checked.Libraries,
		LibraryConfigSchemas: checked.LibraryConfigSchemas,
		GoImports:            generated.GoImports, GoModules: checked.GoModules,
		UBImports: generated.UBImports, UBPackages: generated.UBPackages,
		Assets: checked.Assets, RootAssetSetID: checked.RootAssetSetID,
		Compatibility: checked.Compatibility, LibraryMetadata: checked.LibraryMetadata,
	}, nil
}

func factoryBodyAssets(body *syntax.FactoryBody) []syntax.AssetDecl {
	if body == nil {
		return nil
	}
	return body.Assets
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

func bodyLibraryConfigDeps(
	body *syntax.FactoryBody,
) ([]resolve.SyntaxDependency, error) {
	if body == nil {
		return nil, nil
	}
	deps, errs := resolve.ExtractSyntaxBodyLibraryConfigDeps(*body)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return deps, nil
}

type importVisitor struct {
	resolver                resolve.Resolver
	mode                    Mode
	versions                map[string]string
	validateCompositeBodies bool
	goModules               map[string]string
	ubLibraries             []program.Library
	runtimeLibraries        map[string]*runtime.Library
	reporter                diagnostic.Reporter
	schemas                 *SchemaCache
	assets                  *asset.Collection
	generatedOutputPath     string
}

func newImportVisitor(opts ImportAnalysisOptions, schemas *SchemaCache) *importVisitor {
	return &importVisitor{
		resolver:                opts.Resolver,
		mode:                    opts.Mode,
		versions:                opts.Versions,
		validateCompositeBodies: opts.ValidateCompositeBodies,
		goModules:               map[string]string{},
		runtimeLibraries:        map[string]*runtime.Library{},
		reporter:                opts.Reporter,
		schemas:                 schemas,
		assets:                  &asset.Collection{},
		generatedOutputPath:     opts.GeneratedOutputPath,
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

func (v *importVisitor) resolveBodyLibraryConfigSchemas(
	body syntax.FactoryBody,
	parent *resolve.Source,
) (map[string]runtime.LibraryConfigSchema, error) {
	deps, err := bodyLibraryConfigDeps(&body)
	if err != nil {
		return nil, err
	}
	return v.resolveLibraryConfigDeps(deps, parent)
}

func (v *importVisitor) resolveLibraryConfigDeps(
	deps []resolve.SyntaxDependency,
	parent *resolve.Source,
) (map[string]runtime.LibraryConfigSchema, error) {
	if len(deps) == 0 {
		return nil, nil
	}
	out := map[string]runtime.LibraryConfigSchema{}
	for _, dep := range deps {
		if dep.Kind != resolve.SyntaxDependencyLibraryConfig {
			continue
		}
		if _, done := out[dep.Path]; done {
			continue
		}
		schema, err := v.resolveLibraryConfigDep(dep, parent)
		if err != nil {
			return nil, diagnostic.Context(
				fmt.Sprintf("%s %q", dep.Kind, dep.Label), err,
			)
		}
		out[dep.Path] = schema
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func (v *importVisitor) resolveLibraryConfigDep(
	dep resolve.SyntaxDependency,
	parent *resolve.Source,
) (runtime.LibraryConfigSchema, error) {
	if v.resolver == nil {
		return runtime.LibraryConfigSchema{},
			errors.New("sourcecheck: resolver is required for schema dependencies")
	}
	ref := resolve.ProjectLockVersion(dep.Ref, v.versions)
	remote, isRemote := ref.(*resolve.RemoteImport)
	if isRemote && remote.Version == "" {
		return runtime.LibraryConfigSchema{}, fmt.Errorf(
			"no version for %s in project-lock.ub; run `unobin deps sync`",
			dep.Path,
		)
	}
	source, err := resolve.ResolveImportFrom(v.resolver, ref, parent)
	if err != nil {
		return runtime.LibraryConfigSchema{}, err
	}
	if isRemote {
		if err := resolve.CheckRemotePackageBoundary(remote, source); err != nil {
			return runtime.LibraryConfigSchema{}, err
		}
	}
	classification := resolve.ClassifySource(source)
	if classification.Kind != resolve.SourceGoLibrary {
		return runtime.LibraryConfigSchema{}, libraryConfigSourceError(dep.Path, classification)
	}
	if isRemote && source.ModulePath != "" {
		if err := resolve.ValidateGoModulePath(remote, source.ModulePath); err != nil {
			return runtime.LibraryConfigSchema{}, err
		}
	}
	if source.Path == "" && v.mode == ModeNoFetch {
		return runtime.LibraryConfigSchema{}, nil
	}
	schema, warnings, err := v.schemas.ReadLibraryConfiguration(source.Path)
	if err != nil {
		return runtime.LibraryConfigSchema{}, err
	}
	reportSchemaWarnings(v.reporter, dep.Label, warnings)
	out, ok := runtime.LibraryConfigSchemaFromLibrarySchema(dep.Path, schema)
	if !ok {
		return runtime.LibraryConfigSchema{},
			errors.New("configuration schema is unreadable")
	}
	return out, nil
}

func libraryConfigSourceError(
	path string,
	classification resolve.SourceClassification,
) error {
	switch classification.Kind {
	case resolve.SourceFactory:
		return fmt.Errorf("%s is a factory", path)
	case resolve.SourceUBLibrary:
		return fmt.Errorf("%s is a UB library", path)
	default:
		return fmt.Errorf("%s is not a Go package", path)
	}
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

	composites, err := v.buildCompiledComposites(entries, lib.BodyImports, lib.Source)
	if err != nil {
		return err
	}
	runtimeLib := runtimeLibraryForCompiledComposites(alias, composites)
	library := libraryForCompiledComposites(alias, canonicalKey, composites, lib.SourceFiles)
	v.ubLibraries = append(v.ubLibraries, library)
	v.runtimeLibraries[canonicalKey] = runtimeLib
	return nil
}

type compiledComposite struct {
	data    program.Composite
	goSpecs map[string]program.LibrarySpec
}

func (v *importVisitor) buildCompiledComposites(
	entries []resolve.CompositeEntry,
	bodyImports map[string]map[string][]resolve.Resolution,
	source *resolve.Source,
) ([]compiledComposite, error) {
	composites := make([]compiledComposite, 0, len(entries))
	for _, entry := range entries {
		set, err := asset.Capture(
			source,
			entry.SourceFile,
			entry.SyntaxBody.Assets,
			v.generatedOutputPath,
		)
		if err != nil {
			return nil, diagnostic.Context(
				fmt.Sprintf("%s composite %q", entry.Kind, entry.Name),
				err,
			)
		}
		if err := v.assets.Add(set); err != nil {
			return nil, err
		}
		resols := bodyImports[entry.Kind][entry.Name]
		composite := compiledComposite{data: program.Composite{
			Category: entry.Kind, Export: entry.Name, Body: entry.SyntaxBody,
			Imports: resols, Libraries: make(map[string]*runtime.Library, len(resols)),
		}}
		if set != nil {
			composite.data.AssetSetID = set.ID
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
		libraryConfigSchemas, err := v.resolveBodyLibraryConfigSchemas(
			entry.SyntaxBody,
			source,
		)
		if err != nil {
			return nil, diagnostic.Context(
				fmt.Sprintf("%s composite %q", entry.Kind, entry.Name), err,
			)
		}
		composite.data.LibraryConfigSchemas = libraryConfigSchemas
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
		return diagnostic.Context(fmt.Sprintf(
			"%s composite %q import %q", entry.Kind, entry.Name, res.LocalAlias,
		), err)
	}
	reportSchemaWarnings(v.reporter, res.LocalAlias, warnings)
	composite.data.Libraries[res.LocalAlias] = &runtime.Library{Schema: schema}
	specs := program.LibrarySpec{
		Constraints: constraintsFromSchema(schema), Defaults: defaultsFromSchema(schema),
		Schema: schema,
	}
	if specs.Empty() {
		return nil
	}
	used := bodyUsed[res.LocalAlias]
	specs.Constraints = keepUsedTypes(specs.Constraints, used)
	specs.Defaults = keepUsedTypes(specs.Defaults, used)
	specs.Schema = keepUsedSchema(schema, used)
	if !specs.Empty() {
		if composite.goSpecs == nil {
			composite.goSpecs = map[string]program.LibrarySpec{}
		}
		composite.goSpecs[res.Path] = specs
	}
	return nil
}

func (v *importVisitor) addCompiledUBImport(
	composite *compiledComposite,
	res resolve.Resolution,
) error {
	composite.data.Libraries[res.LocalAlias] = v.runtimeLibraries[res.CanonicalKey]
	return nil
}

func runtimeLibraryForCompiledComposites(
	name string,
	composites []compiledComposite,
) *runtime.Library {
	lib := &runtime.Library{Name: name}
	for _, composite := range composites {
		syntaxBody := composite.data.Body
		lib.AddComposite(&runtime.CompositeType{
			Name:                 composite.data.Export,
			Kind:                 runtime.NodeKind(composite.data.Category),
			SyntaxBody:           &syntaxBody,
			Libraries:            composite.data.Libraries,
			LibraryConfigSchemas: composite.data.LibraryConfigSchemas,
			AssetSetID:           composite.data.AssetSetID,
		})
	}
	return lib
}

func libraryForCompiledComposites(
	name, canonicalKey string,
	composites []compiledComposite,
	sourceFiles map[string]syntax.SourceFileSpec,
) program.Library {
	library := program.Library{
		Name: name, CanonicalKey: canonicalKey, SourceFiles: sourceFiles,
		Composites: make([]program.Composite, 0, len(composites)),
		Specs:      goSpecsForCompiledComposites(composites),
	}
	for _, composite := range composites {
		library.Composites = append(library.Composites, composite.data)
	}
	return library
}

func goSpecsForCompiledComposites(
	composites []compiledComposite,
) map[string]program.LibrarySpec {
	out := map[string]program.LibrarySpec{}
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
	out map[string]program.LibrarySpec,
	importPath string,
	specs program.LibrarySpec,
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
	copyConfigurationSchema(dst, src)
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
	copyConfigurationSchema(out, schema)
	if len(out.Resources)+len(out.DataSources)+len(out.Actions) == 0 &&
		!out.HasConfiguration {
		return nil
	}
	return out
}

func copyConfigurationSchema(dst, src *runtime.LibrarySchema) {
	if src == nil || !src.HasConfiguration {
		return
	}
	dst.HasConfiguration = src.HasConfiguration
	dst.Configuration = maps.Clone(src.Configuration)
	dst.ConfigurationFields = slices.Clone(src.ConfigurationFields)
	dst.ConfigurationDefaults = slices.Clone(src.ConfigurationDefaults)
	dst.ConfigurationConstraints = slices.Clone(src.ConfigurationConstraints)
	dst.ConfigurationIdentity = src.ConfigurationIdentity
	dst.ConfigurationDigest = src.ConfigurationDigest
	dst.ConfigurationEmpty = src.ConfigurationEmpty
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
