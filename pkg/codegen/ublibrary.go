package codegen

import (
	"bytes"
	"fmt"
	"go/format"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// GoLibrarySpecs holds one Go library's compile-extracted spec data,
// keyed by "<kind>.<type>" the way runtime.Library stores it. The dev
// CLI gathers it from the library's source; codegen embeds it in
// generated code so the runtime can look it up at plan and apply.
type GoLibrarySpecs struct {
	Constraints map[string][]lang.ConstraintSpec
	Defaults    map[string][]lang.DefaultSpec
	Schema      *runtime.LibrarySchema
}

// Empty reports whether the specs hold no data at all.
func (s GoLibrarySpecs) Empty() bool {
	return len(s.Constraints) == 0 && len(s.Defaults) == 0 && !schemaHasRuntimeData(s.Schema)
}

// GenerateUBLibrary produces the Go source for a UB library's
// generated package. The package's name is alias; it exports a
// `Library()` function returning a `*runtime.Library` whose composites
// are split into one map per kind (ResourceComposites, DataComposites,
// ActionComposites). syntaxBodies are keyed by kind and then composite
// name; each generated registration uses SyntaxBody.
//
// imports maps each composite's kind and name to its resolved import table:
// the composite's body's own imports block, with each declared alias
// mapped to its canonical library path. Generated composites declare
// LibraryBindings for the factory catalog to resolve. Pass nil or an
// empty map when a composite has no imports.
func GenerateUBLibrary(
	alias string,
	syntaxBodies map[string]map[string]syntax.FactoryBody,
	imports map[string]map[string]map[string]string,

) ([]byte, error) {
	return GenerateUBLibraryPackage(alias, alias, syntaxBodies, imports, nil)
}

// GenerateUBLibraryPackage produces a UB library package whose Go package
// identifier can differ from the runtime library name.
func GenerateUBLibraryPackage(
	packageID string,
	libraryName string,
	syntaxBodies map[string]map[string]syntax.FactoryBody,
	imports map[string]map[string]map[string]string,

	sourceFiles map[string]syntax.SourceFileSpec,
) ([]byte, error) {
	return generateUBLibraryPackage(
		packageID,
		libraryName,
		syntaxBodies,
		imports,

		sourceFiles,
		nil,
		nil,
	)
}

// GenerateUBLibraryPackageWithAssets includes the captured asset-set ID
// for each composite body.
func GenerateUBLibraryPackageWithAssets(
	packageID string,
	libraryName string,
	syntaxBodies map[string]map[string]syntax.FactoryBody,
	imports map[string]map[string]map[string]string,

	sourceFiles map[string]syntax.SourceFileSpec,
	assetSetIDs map[string]map[string]string,
) ([]byte, error) {
	return GenerateUBLibraryPackageWithAssetsAndConfigSchemas(
		packageID,
		libraryName,
		syntaxBodies,
		imports,

		sourceFiles,
		assetSetIDs,
		nil,
	)
}

// GenerateUBLibraryPackageWithAssetsAndConfigSchemas includes the captured
// asset-set IDs and resolved library-config schemas for each composite body.
func GenerateUBLibraryPackageWithAssetsAndConfigSchemas(
	packageID string,
	libraryName string,
	syntaxBodies map[string]map[string]syntax.FactoryBody,
	imports map[string]map[string]map[string]string,

	sourceFiles map[string]syntax.SourceFileSpec,
	assetSetIDs map[string]map[string]string,
	libraryConfigSchemas map[string]map[string]map[string]runtime.LibraryConfigSchema,
) ([]byte, error) {
	return generateUBLibraryPackage(
		packageID,
		libraryName,
		syntaxBodies,
		imports,

		sourceFiles,
		assetSetIDs,
		libraryConfigSchemas,
	)
}

func generateUBLibraryPackage(
	packageID string,
	libraryName string,
	syntaxBodies map[string]map[string]syntax.FactoryBody,
	imports map[string]map[string]map[string]string,

	sourceFiles map[string]syntax.SourceFileSpec,
	assetSetIDs map[string]map[string]string,
	libraryConfigSchemas map[string]map[string]map[string]runtime.LibraryConfigSchema,
) ([]byte, error) {
	if packageID == "" {
		return nil, fmt.Errorf("ublibrary: package name is required")
	}
	if libraryName == "" {
		return nil, fmt.Errorf("ublibrary: library name is required")
	}

	sourceHelpers, sourceHelperByFile := sourceHelpersFor(sourceFiles)
	hasConfigSchemaLang := false
	hasConfigSchemaTypecheck := false
	groups := map[string]*compositeGroup{}
	for _, c := range compositeKinds {
		groups[c.kind] = &compositeGroup{MapField: c.mapField, Symbol: c.symbol}
	}
	for _, kind := range compositeKindNames(syntaxBodies) {
		group, ok := groups[kind]
		if !ok {
			return nil, fmt.Errorf("ublibrary %q: unknown kind %q", libraryName, kind)
		}
		for _, name := range compositeNames(syntaxBodies[kind]) {
			configSchemas := libraryConfigSchemas[kind][name]
			entry := compositeEntry{
				Name:       name,
				Symbol:     group.Symbol,
				AssetSetID: assetSetIDs[kind][name],
			}
			if len(configSchemas) > 0 {
				entry.LibraryConfigSchemas = libraryConfigSchemasLiteral(configSchemas)
				hasConfigSchemaLang = hasConfigSchemaLang ||
					libraryConfigSchemasNeedLang(configSchemas)
				hasConfigSchemaTypecheck = hasConfigSchemaTypecheck ||
					libraryConfigSchemasNeedTypecheck(configSchemas)
			}
			encoded, err := encodeSyntaxBodyWithSourceHelpers(
				syntaxBodies[kind][name], sourceHelperByFile)
			if err != nil {
				return nil, fmt.Errorf("ublibrary %q: encode %s %q syntax body: %w",
					libraryName, kind, name, err)
			}
			entry.SyntaxBody = "&" + encoded
			for _, localAlias := range sortedAliases(imports[kind][name]) {
				p := imports[kind][name][localAlias]
				entry.Libraries = append(entry.Libraries, libraryBinding{
					LocalAlias: localAlias,
					Path:       p,
				})
			}
			group.Entries = append(group.Entries, entry)
		}
	}

	orderedGroups := make([]*compositeGroup, 0, len(compositeKinds))
	for _, c := range compositeKinds {
		if g := groups[c.kind]; len(g.Entries) > 0 {
			orderedGroups = append(orderedGroups, g)
		}
	}

	var buf bytes.Buffer
	data := struct {
		PackageName      string
		LibraryName      string
		Groups           []*compositeGroup
		SourceHelpers    []sourceHelper
		HasLang          bool
		HasTypecheck     bool
		HasSyntaxBodies  bool
		HasSourceHelpers bool
	}{
		PackageName:   sanitizeIdent(packageID),
		LibraryName:   libraryName,
		Groups:        orderedGroups,
		SourceHelpers: sourceHelpers,
		HasLang: hasSyntaxBodies(orderedGroups) ||
			hasConfigSchemaLang,
		HasTypecheck:     hasConfigSchemaTypecheck,
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

// compositeKinds lists the kinds in the order the generated Library()
// emits their maps, with the runtime field and NodeKind symbol for
// each.
var compositeKinds = []struct {
	kind     string
	mapField string
	symbol   string
}{
	{"resource", "ResourceComposites", "runtime.NodeResource"},
	{"data-source", "DataComposites", "runtime.NodeDataSource"},
	{"action", "ActionComposites", "runtime.NodeAction"},
}

type compositeGroup struct {
	MapField string
	Symbol   string
	Entries  []compositeEntry
}

type compositeEntry struct {
	Name                 string
	Symbol               string
	SyntaxBody           string
	AssetSetID           string
	LibraryConfigSchemas string
	Libraries            []libraryBinding
}

type sourceHelper struct {
	VarName     string
	FuncName    string
	DisplayPath string
	LineStarts  string
}

func sourceHelpersFor(
	sourceFiles map[string]syntax.SourceFileSpec,
) ([]sourceHelper, map[string]sourceHelper) {
	if len(sourceFiles) == 0 {
		return nil, nil
	}
	files := make([]string, 0, len(sourceFiles))
	for file := range sourceFiles {
		files = append(files, file)
	}
	slices.Sort(files)
	helpers := make([]sourceHelper, 0, len(files))
	byFile := make(map[string]sourceHelper, len(files))
	for i, file := range files {
		spec := sourceFiles[file]
		helper := sourceHelper{
			VarName:     fmt.Sprintf("source%d", i),
			FuncName:    fmt.Sprintf("sp%d", i),
			DisplayPath: factorySourceDisplayPath(spec),
			LineStarts:  intSliceLiteral(spec.LineStarts),
		}
		helpers = append(helpers, helper)
		byFile[file] = helper
	}
	return helpers, byFile
}

func encodeSyntaxBodyWithSourceHelpers(
	body syntax.FactoryBody,
	helpers map[string]sourceHelper,
) (string, error) {
	if len(helpers) == 0 {
		return EncodeSyntaxFactoryBody(body)
	}
	missing := map[string]bool{}
	encoded, err := EncodeSyntaxFactoryBodyWithSpans(body, func(s parse.Span) string {
		helper, ok := helpers[s.Start.File]
		if !ok {
			missing[s.Start.File] = true
			return "parse.Span{}"
		}
		return fmt.Sprintf("%s(%d, %d)", helper.FuncName, s.Start.Offset, s.End.Offset)
	})
	if err != nil {
		return "", err
	}
	if len(missing) > 0 {
		files := make([]string, 0, len(missing))
		for file := range missing {
			files = append(files, file)
		}
		slices.Sort(files)
		return "", fmt.Errorf("missing source metadata for %s", strings.Join(files, ", "))
	}
	return encoded, nil
}

func compositeKindNames(syntaxBodies map[string]map[string]syntax.FactoryBody) []string {
	out := make([]string, 0, len(syntaxBodies))
	for kind := range syntaxBodies {
		out = append(out, kind)
	}
	slices.Sort(out)
	return out
}

func compositeNames(syntaxBodies map[string]syntax.FactoryBody) []string {
	out := make([]string, 0, len(syntaxBodies))
	for name := range syntaxBodies {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

type libraryBinding struct {
	LocalAlias string
	Path       string
}

// identTable assigns one Go-level identifier to each unique import
// path. The first time a path is seen, the ident is `lib_` followed
// by the path's last component sanitized into a valid Go identifier;
// later paths whose sanitized last component would collide get a
// numeric suffix appended.
type identTable struct {
	byPath map[string]string
	used   map[string]bool
	order  []string
}

func newIdentTable() *identTable {
	return &identTable{
		byPath: map[string]string{},
		used:   map[string]bool{},
	}
}

func (t *identTable) identFor(p string) string {
	if id, ok := t.byPath[p]; ok {
		return id
	}
	base := "lib_" + sanitizeIdent(path.Base(p))
	id := base
	for i := 2; t.used[id]; i++ {
		id = fmt.Sprintf("%s_%d", base, i)
	}
	t.byPath[p] = id
	t.used[id] = true
	t.order = append(t.order, p)
	return id
}

func sanitizeIdent(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '-' || r == '.':
			b.WriteRune('_')
		case r >= '0' && r <= '9':
			if i == 0 {
				b.WriteRune('_')
			}
			b.WriteRune(r)
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "x"
	}
	out := b.String()
	if token.Lookup(out).IsKeyword() {
		return "_" + out
	}
	return out
}

func hasSyntaxBodies(groups []*compositeGroup) bool {
	for _, group := range groups {
		for _, entry := range group.Entries {
			if entry.SyntaxBody != "" {
				return true
			}
		}
	}
	return false
}

func sortedAliases(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

var ubLibraryTemplate = template.Must(template.New("ublibrary.go").Funcs(template.FuncMap{
	"quote": strconv.Quote,
}).Parse(`// Code generated by unobin. DO NOT EDIT.
package {{.PackageName}}

import (
{{if .HasLang}}	"github.com/cloudboss/unobin/pkg/lang"
{{end}}{{if .HasSourceHelpers}}	"github.com/cloudboss/unobin/pkg/lang/parse"
{{end}}{{if .HasSyntaxBodies}}	"github.com/cloudboss/unobin/pkg/lang/syntax"
{{end}}	"github.com/cloudboss/unobin/pkg/runtime"
{{if .HasTypecheck}}	"github.com/cloudboss/unobin/pkg/typecheck"
{{end}})

{{range .SourceHelpers}}var {{.VarName}} = parse.NewSourceFile(
	{{quote .DisplayPath}},
	{{.LineStarts}},
)

func {{.FuncName}}(start, end int) parse.Span {
	return {{.VarName}}.Span(start, end)
}

{{end}}func Library() *runtime.Library {
	return &runtime.Library{
		Name: {{quote .LibraryName}},
{{range .Groups}}		{{.MapField}}: map[string]*runtime.CompositeType{
{{range .Entries}}			{{quote .Name}}: {
				Name: {{quote .Name}},
				Kind: {{.Symbol}},
{{- if .AssetSetID}}
				AssetSetID: {{quote .AssetSetID}},
{{- end}}
{{- if .SyntaxBody}}
				SyntaxBody: {{.SyntaxBody}},
{{- end}}
{{- if .LibraryConfigSchemas}}
				LibraryConfigSchemas: {{.LibraryConfigSchemas}},
{{- end}}
{{- if .Libraries}}
				LibraryBindings: map[string]string{
{{range .Libraries}}					{{quote .LocalAlias}}: {{quote .Path}},
{{end}}				},
{{- end}}
			},
{{end}}		},
{{end}}	}
}
`))
