package codegen

import (
	"bytes"
	"fmt"
	"go/format"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/libraryapi"
	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func GenerateLibrary(
	packageID string,
	library program.Library,
	packagePaths map[string]string,
) ([]byte, error) {
	if packageID == "" {
		return nil, fmt.Errorf("ublibrary: package name is required")
	}
	if library.Name == "" {
		return nil, fmt.Errorf("ublibrary: library name is required")
	}

	idents := newIdentTable()
	sourceHelpers, sourceHelperByFile := sourceHelpersFor(library.SourceFiles)
	hasConfigSchemaLang := false
	hasConfigSchemaTypecheck := false
	hasBodyLang := false
	groups := map[string]*compositeGroup{}
	for _, c := range compositeKinds {
		groups[c.kind] = &compositeGroup{MapField: c.mapField, Symbol: c.symbol}
	}
	compositeOrder := func(a, b program.Composite) int {
		if category := strings.Compare(a.Category, b.Category); category != 0 {
			return category
		}
		return strings.Compare(a.Export, b.Export)
	}
	composites := library.Composites
	if !slices.IsSortedFunc(composites, compositeOrder) {
		composites = slices.Clone(composites)
		slices.SortFunc(composites, compositeOrder)
	}
	for i, composite := range composites {
		category, name := composite.Category, composite.Export
		group, ok := groups[category]
		if !ok {
			return nil, fmt.Errorf("ublibrary %q: unknown kind %q", library.Name, category)
		}
		if i > 0 && composites[i-1].Category == category && composites[i-1].Export == name {
			return nil, fmt.Errorf("ublibrary %q: duplicate %s export %q",
				library.Name, category, name)
		}
		configSchemas := composite.LibraryConfigSchemas
		entry := compositeEntry{Name: name, Symbol: group.Symbol, AssetSetID: composite.AssetSetID}
		if len(configSchemas) > 0 {
			entry.LibraryConfigSchemas = libraryConfigSchemasLiteral(configSchemas)
			hasConfigSchemaLang = hasConfigSchemaLang || libraryConfigSchemasNeedLang(configSchemas)
			hasConfigSchemaTypecheck = hasConfigSchemaTypecheck ||
				libraryConfigSchemasNeedTypecheck(configSchemas)
		}
		encoded, err := encodeSyntaxBodyWithSourceHelpers(composite.Body, sourceHelperByFile)
		if err != nil {
			return nil, fmt.Errorf("ublibrary %q: encode %s %q syntax body: %w",
				library.Name, category, name, err)
		}
		entry.SyntaxBody = "&" + encoded.Literal
		hasBodyLang = hasBodyLang || encoded.UsesLang
		importOrder := func(a, b resolve.Resolution) int {
			return strings.Compare(a.LocalAlias, b.LocalAlias)
		}
		imports := composite.Imports
		if !slices.IsSortedFunc(imports, importOrder) {
			imports = slices.Clone(imports)
			slices.SortFunc(imports, importOrder)
		}
		for j, imported := range imports {
			if j > 0 && imports[j-1].LocalAlias == imported.LocalAlias {
				return nil, fmt.Errorf("ublibrary %q: %s export %q: duplicate import alias %q",
					library.Name, category, name, imported.LocalAlias)
			}
			importPath := imported.Path
			switch imported.Kind {
			case resolve.ResolutionGo:
			case resolve.ResolutionUB:
				importPath = packagePaths[imported.CanonicalKey]
			default:
				return nil, fmt.Errorf("ublibrary %q: %s export %q: unknown import kind %v",
					library.Name, category, name, imported.Kind)
			}
			if importPath == "" {
				return nil, fmt.Errorf(
					"ublibrary %q: %s export %q: missing package path for import %q",
					library.Name, category, name, imported.LocalAlias)
			}
			entry.Libraries = append(entry.Libraries, libraryBinding{
				LocalAlias: imported.LocalAlias, Path: importPath,
				GoIdent: idents.identFor(importPath),
			})
		}
		group.Entries = append(group.Entries, entry)
	}

	orderedGroups := make([]*compositeGroup, 0, len(compositeKinds))
	for _, c := range compositeKinds {
		if g := groups[c.kind]; len(g.Entries) > 0 {
			orderedGroups = append(orderedGroups, g)
		}
	}

	specVars, varOf := specVarsFor(idents, library.Specs)
	for _, g := range orderedGroups {
		for _, entry := range g.Entries {
			for i, b := range entry.Libraries {
				if name, ok := varOf[b.Path]; ok {
					entry.Libraries[i].Value = name
				} else {
					entry.Libraries[i].Value = b.GoIdent + ".Library()"
				}
			}
		}
	}

	var buf bytes.Buffer
	data := struct {
		PackageName      string
		LibraryName      string
		RequiredAPI      string
		SpecVars         []specVar
		Groups           []*compositeGroup
		GoImports        []goImport
		SourceHelpers    []sourceHelper
		HasLang          bool
		HasTypecheck     bool
		HasSyntaxBodies  bool
		HasSourceHelpers bool
	}{
		PackageName:   sanitizeIdent(packageID),
		LibraryName:   library.Name,
		RequiredAPI:   libraryapi.Current().GeneratorAPI,
		SpecVars:      specVars,
		Groups:        orderedGroups,
		GoImports:     idents.imports(),
		SourceHelpers: sourceHelpers,
		HasLang: specVarsNeedLang(specVars) || hasBodyLang ||
			hasConfigSchemaLang,
		HasTypecheck:     specVarsNeedTypecheck(specVars) || hasConfigSchemaTypecheck,
		HasSyntaxBodies:  hasSyntaxBodies(orderedGroups),
		HasSourceHelpers: len(sourceHelpers) > 0,
	}
	if err := ubLibraryTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("ublibrary: %w", err)
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("ublibrary: format generated source: %w", err)
	}
	return out, nil
}
