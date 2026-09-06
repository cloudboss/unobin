package codegen

import (
	"fmt"
	"maps"
	"slices"

	"github.com/cloudboss/unobin/pkg/runtime"
)

type factoryCatalogLibrary struct {
	LibraryPath string
	GoIdent     string
	Constraints string
	Defaults    string
	Schema      string
}

type linkedFactoryCatalog struct {
	Imports       []factoryPackageImport
	Bindings      []aliasImport
	Registrations []factoryCatalogLibrary
	HasLang       bool
	HasTypecheck  bool
}

func linkFactoryCatalog(in Input) (linkedFactoryCatalog, error) {
	root, err := linkFactoryLibraries(in.GoImports, in.UBImports)
	if err != nil {
		return linkedFactoryCatalog{}, err
	}
	imports := in.CatalogImports
	bindings := in.LibraryBindings
	if imports == nil {
		imports = map[string]string{}
		bindings = map[string]string{}
		for _, binding := range root.Bindings {
			imports[binding.Path] = binding.Path
			bindings[binding.LocalAlias] = binding.Path
		}
	}
	specs := make(map[string]GoLibrarySpecs, len(imports))
	for _, path := range slices.Sorted(maps.Keys(in.CatalogSpecs)) {
		if _, exists := imports[path]; !exists {
			return linkedFactoryCatalog{}, fmt.Errorf(
				"codegen: metadata library %q is unavailable", path,
			)
		}
		spec := in.CatalogSpecs[path]
		specs[path] = GoLibrarySpecs{
			Constraints: maps.Clone(spec.Constraints), Defaults: maps.Clone(spec.Defaults),
			Schema: mergeGeneratedLibrarySchema(nil, spec.Schema),
		}
	}
	for _, binding := range root.Bindings {
		alias := binding.LocalAlias
		path := bindings[alias]
		if imports[path] != binding.Path {
			return linkedFactoryCatalog{}, fmt.Errorf(
				"codegen: import %q does not match its catalog registration", alias,
			)
		}
		spec := specs[path]
		spec.Constraints = mergeGeneratedMap(spec.Constraints, in.GoConstraints[alias])
		spec.Defaults = mergeGeneratedMap(spec.Defaults, in.GoDefaults[alias])
		spec.Schema = mergeGeneratedLibrarySchema(spec.Schema, in.GoSchemas[alias])
		specs[path] = spec
	}
	idents := newIdentTable()
	for _, imported := range root.Imports {
		idents.byPath[imported.Path] = imported.GoIdent
		idents.used[imported.GoIdent] = true
	}
	linked := linkedFactoryCatalog{}
	pathsByPackage := map[string]string{}
	for _, path := range sortedKeys(imports) {
		if err := (runtime.Binding{LibraryPath: path, Export: "library"}).Validate(); err != nil {
			return linkedFactoryCatalog{}, fmt.Errorf("codegen: catalog: %w", err)
		}
		goPath := imports[path]
		if goPath == "" {
			return linkedFactoryCatalog{}, fmt.Errorf("codegen: library %q Go path is required", path)
		}
		if _, exists := pathsByPackage[goPath]; exists {
			return linkedFactoryCatalog{}, fmt.Errorf(
				"codegen: Go package %q has multiple library paths", goPath,
			)
		}
		pathsByPackage[goPath] = path
		ident := idents.identFor(goPath)
		linked.Imports = append(linked.Imports, factoryPackageImport{GoIdent: ident, Path: goPath})
		registration := factoryCatalogLibrary{LibraryPath: path, GoIdent: ident}
		spec := specs[path]
		if len(spec.Constraints) > 0 {
			registration.Constraints = constraintsAssign("library", spec.Constraints)
		}
		if len(spec.Defaults) > 0 {
			registration.Defaults = defaultsAssign("library", spec.Defaults)
		}
		if schemaHasRuntimeData(spec.Schema) {
			registration.Schema = schemaAssign("library", spec.Schema)
		}
		linked.Registrations = append(linked.Registrations, registration)
		linked.HasLang = linked.HasLang || len(spec.Constraints)+len(spec.Defaults) > 0 ||
			schemaNeedsLang(spec.Schema)
		linked.HasTypecheck = linked.HasTypecheck || schemaNeedsTypecheck(spec.Schema)
	}
	for _, alias := range sortedKeys(bindings) {
		if alias == "" {
			return linkedFactoryCatalog{}, fmt.Errorf("codegen: library import alias is required")
		}
		path := bindings[alias]
		if _, exists := imports[path]; !exists {
			return linkedFactoryCatalog{}, fmt.Errorf("codegen: library %q is unavailable", path)
		}
		linked.Bindings = append(linked.Bindings, aliasImport{LocalAlias: alias, Path: path})
	}
	return linked, nil
}
