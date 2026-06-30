package codegen

import (
	"bytes"
	"fmt"
	"go/format"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// Input bundles everything codegen needs to produce a factory binary's
// `main.go`. FactoryBody is the typed factory body the binary executes.
// LibraryPath is the binary's library-path identity, the same form Go libraries
// use; the operator's stack file asserts the same value under
// `factory.pin.library-path` and plan, refresh, and validate refuse on
// mismatch. An empty LibraryPath disables that identity check. The version and
// content-revision are not generated here; compile stamps them into the built
// binary with -ldflags so the generated source stays a pure function of the
// factory content.
// GoImports maps each Go-library alias the source uses to the Go import
// path that supplies it (e.g.,
// `"std" -> "github.com/cloudboss/unobin-library-std"`).
// GoModules maps each required Go module path to the selected version for
// go.mod. A Go package import below a module appears only in GoImports.
// UBImports maps each UB-library alias to the local Go import path of
// the package that compile generated for it (typically
// `<factory-name>/internal/<alias>`).
type Input struct {
	FactoryBody   syntax.FactoryBody
	FactorySource syntax.SourceFileSpec
	// Body is a test convenience for callers that have a small source fragment.
	Body        string
	LibraryPath string
	FactoryName string
	GoImports   map[string]string
	GoModules   map[string]string
	UBImports   map[string]string
	// GoConstraints maps a Go-library alias to its types' cross-field
	// constraints (kebab type name -> specs), gathered by the dev CLI
	// from the library's source. codegen attaches them to the library in
	// the generated main.go so the plan can check each node against them.
	GoConstraints map[string]map[string][]lang.ConstraintSpec
	// GoDefaults maps a Go-library alias to its types' declared input
	// defaults, gathered the same way and attached the same way, so the
	// runtime can fill them into evaluated bodies.
	GoDefaults map[string]map[string][]lang.DefaultSpec
	// GoSchemas maps a Go-library alias to schema metadata the compiled
	// runtime needs after compile-time checks, such as sensitive fields.
	GoSchemas map[string]*runtime.LibrarySchema
}

// Generate produces the formatted Go source for the factory binary's
// main.go. The result is the bytes a caller writes to disk and feeds
// through `go build`.
func Generate(in Input) ([]byte, error) {
	if in.FactoryName == "" {
		return nil, fmt.Errorf("codegen: FactoryName is required")
	}
	goImports := aliasImports(in.GoImports)
	ubImports := aliasImports(in.UBImports)
	constraintAliases := injectedAliases(in.GoConstraints)
	defaultAliases := injectedAliases(in.GoDefaults)
	schemaInjectAliases := schemaAliases(in.GoSchemas)
	body, source, err := factoryBodyInput(in)
	if err != nil {
		return nil, err
	}
	factoryBody, err := EncodeSyntaxFactoryBodyWithSpans(body, func(s parse.Span) string {
		return fmt.Sprintf("sp(%d, %d)", s.Start.Offset, s.End.Offset)
	})
	if err != nil {
		return nil, err
	}
	data := struct {
		FactoryBodyLiteral string
		FactorySourcePath  string
		FactoryLineStarts  string
		LibraryPath        string
		FactoryName        string
		GoImports          []aliasImport
		UBImports          []aliasImport
		ConstraintAliases  []string
		GoConstraints      map[string]map[string][]lang.ConstraintSpec
		DefaultAliases     []string
		GoDefaults         map[string]map[string][]lang.DefaultSpec
		SchemaAliases      []string
		GoSchemas          map[string]*runtime.LibrarySchema
		HasLang            bool
		HasTypecheck       bool
		Inject             bool
	}{
		FactoryBodyLiteral: factoryBody,
		FactorySourcePath:  factorySourceDisplayPath(source),
		FactoryLineStarts:  intSliceLiteral(source.LineStarts),
		LibraryPath:        in.LibraryPath,
		FactoryName:        in.FactoryName,
		GoImports:          goImports,
		UBImports:          ubImports,
		ConstraintAliases:  constraintAliases,
		GoConstraints:      in.GoConstraints,
		DefaultAliases:     defaultAliases,
		GoDefaults:         in.GoDefaults,
		SchemaAliases:      schemaInjectAliases,
		GoSchemas:          in.GoSchemas,
		HasLang: len(constraintAliases)+len(defaultAliases) > 0 ||
			schemasNeedLang(in.GoSchemas) || strings.Contains(factoryBody, "lang."),
		HasTypecheck: schemasNeedTypecheck(in.GoSchemas),
		Inject:       len(constraintAliases)+len(defaultAliases)+len(schemaInjectAliases) > 0,
	}

	var buf bytes.Buffer
	if err := mainTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("codegen: %w", err)
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("codegen: format generated source: %w", err)
	}
	return out, nil
}

type aliasImport struct {
	LocalAlias string
	GoIdent    string
	Path       string
}

func aliasImports(m map[string]string) []aliasImport {
	aliases := sortedKeys(m)
	used := map[string]bool{}
	out := make([]aliasImport, 0, len(aliases))
	for _, alias := range aliases {
		base := "lib_" + sanitizeIdent(alias)
		ident := base
		for i := 2; used[ident]; i++ {
			ident = fmt.Sprintf("%s_%d", base, i)
		}
		used[ident] = true
		out = append(out, aliasImport{LocalAlias: alias, GoIdent: ident, Path: m[alias]})
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func quote(s string) string {
	return strconv.Quote(s)
}

func factoryBodyInput(in Input) (syntax.FactoryBody, syntax.SourceFileSpec, error) {
	if in.Body == "" {
		return in.FactoryBody, in.FactorySource, nil
	}
	src := []byte(inputFactorySource(in.Body))
	sf, err := syntax.ParseSource("factory.ub", src)
	if err != nil {
		return syntax.FactoryBody{}, syntax.SourceFileSpec{}, err
	}
	if sf.Kind != syntax.FileFactory || sf.Factory == nil {
		return syntax.FactoryBody{}, syntax.SourceFileSpec{},
			fmt.Errorf("factory.ub must declare factory")
	}
	return sf.Factory.Body, syntax.SourceFileSpec{
		DisplayPath:    "factory.ub",
		ProjectRelPath: "factory.ub",
		LineStarts:     parse.LineStarts(src),
	}, nil
}

func inputFactorySource(body string) string {
	if strings.HasPrefix(strings.TrimSpace(body), "factory"+":") {
		return body
	}
	return "factory" + ": {\n" + body + "\n}\n"
}

func factorySourceDisplayPath(spec syntax.SourceFileSpec) string {
	if spec.DisplayPath != "" {
		return spec.DisplayPath
	}
	if spec.ProjectRelPath != "" {
		return spec.ProjectRelPath
	}
	return "factory.ub"
}

func intSliceLiteral(values []int) string {
	if len(values) == 0 {
		return "[]int{0}"
	}
	var b strings.Builder
	b.WriteString("[]int{")
	for i, value := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d", value)
	}
	b.WriteString("}")
	return b.String()
}

// injectedAliases returns the Go-library aliases with at least one
// type declaring specs to inject, sorted for stable output.
func injectedAliases[T any](m map[string]map[string][]T) []string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if len(v) > 0 {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

func schemaAliases(m map[string]*runtime.LibrarySchema) []string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if schemaHasRuntimeData(v) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

func schemasNeedLang(m map[string]*runtime.LibrarySchema) bool {
	for _, schema := range m {
		if schemaNeedsLang(schema) {
			return true
		}
	}
	return false
}

func schemasNeedTypecheck(m map[string]*runtime.LibrarySchema) bool {
	for _, schema := range m {
		if schemaNeedsTypecheck(schema) {
			return true
		}
	}
	return false
}

func schemaHasRuntimeData(schema *runtime.LibrarySchema) bool {
	return schemaHasSensitivity(schema) || schemaHasConfigurationData(schema)
}

func schemaHasConfigurationData(schema *runtime.LibrarySchema) bool {
	return schema != nil && (schema.HasConfiguration || schema.Configuration != nil ||
		len(schema.ConfigurationFields) > 0 || len(schema.ConfigurationDefaults) > 0 ||
		len(schema.ConfigurationConstraints) > 0 || schema.ConfigurationIdentity != "" ||
		schema.ConfigurationDigest != "" || schema.ConfigurationEmpty)
}

func schemaNeedsLang(schema *runtime.LibrarySchema) bool {
	return schema != nil && (len(schema.ConfigurationDefaults) > 0 ||
		len(schema.ConfigurationConstraints) > 0)
}

func schemaNeedsTypecheck(schema *runtime.LibrarySchema) bool {
	return schema != nil && len(schema.ConfigurationFields) > 0
}

func schemaHasSensitivity(schema *runtime.LibrarySchema) bool {
	if schema == nil {
		return false
	}
	return schemaTypesHaveSensitivity(schema.Resources) ||
		schemaTypesHaveSensitivity(schema.DataSources) ||
		schemaTypesHaveSensitivity(schema.Actions)
}

func schemaTypesHaveSensitivity(types map[string]*runtime.TypeSchema) bool {
	for _, ts := range types {
		if ts != nil && (len(ts.SensitiveInputs) > 0 || len(ts.SensitiveOutputs) > 0) {
			return true
		}
	}
	return false
}

// injectConstraints renders the assignment that attaches one Go library's
// constraints to its entry in the libraries map.
func injectConstraints(alias string, all map[string]map[string][]lang.ConstraintSpec) string {
	return constraintsAssign(fmt.Sprintf("libraries[%q]", alias), all[alias])
}

// injectDefaults renders the assignment that attaches one Go library's
// declared input defaults to its entry in the libraries map.
func injectDefaults(alias string, all map[string]map[string][]lang.DefaultSpec) string {
	return defaultsAssign(fmt.Sprintf("libraries[%q]", alias), all[alias])
}

func injectSchema(alias string, all map[string]*runtime.LibrarySchema) string {
	return schemaAssign(fmt.Sprintf("libraries[%q]", alias), all[alias])
}

// constraintsAssign renders the statement attaching constraint specs to
// target: a map literal with one spec per line. format.Source aligns
// and indents the result.
func constraintsAssign(target string, byType map[string][]lang.ConstraintSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s.Constraints = map[string][]lang.ConstraintSpec{\n", target)
	for _, typ := range sortedSpecKeys(byType) {
		fmt.Fprintf(&b, "%q: {\n", typ)
		for _, s := range byType[typ] {
			b.WriteString(specLiteral(s))
			b.WriteString(",\n")
		}
		b.WriteString("},\n")
	}
	b.WriteString("}")
	return b.String()
}

// defaultsAssign renders the statement attaching declared input
// defaults to target, in the same style constraintsAssign uses.
func defaultsAssign(target string, byType map[string][]lang.DefaultSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s.Defaults = map[string][]lang.DefaultSpec{\n", target)
	for _, typ := range sortedSpecKeys(byType) {
		fmt.Fprintf(&b, "%q: {\n", typ)
		for _, s := range byType[typ] {
			b.WriteString(defaultLiteral(s))
			b.WriteString(",\n")
		}
		b.WriteString("},\n")
	}
	b.WriteString("}")
	return b.String()
}

func schemaAssign(target string, schema *runtime.LibrarySchema) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s.Schema = &runtime.LibrarySchema{\n", target)
	schemaMapLiteral(&b, "Resources", schema.Resources)
	schemaMapLiteral(&b, "DataSources", schema.DataSources)
	schemaMapLiteral(&b, "Actions", schema.Actions)
	schemaConfigurationLiteral(&b, schema)
	b.WriteString("}")
	return b.String()
}

func schemaMapLiteral(
	b *strings.Builder,
	field string,
	types map[string]*runtime.TypeSchema,
) {
	if !schemaTypesHaveSensitivity(types) {
		return
	}
	fmt.Fprintf(b, "%s: map[string]*runtime.TypeSchema{\n", field)
	for _, typ := range sortedSchemaTypeKeys(types) {
		fmt.Fprintf(b, "%q: %s,\n", typ, typeSchemaLiteral(types[typ]))
	}
	b.WriteString("},\n")
}

func schemaConfigurationLiteral(b *strings.Builder, schema *runtime.LibrarySchema) {
	if !schemaHasConfigurationData(schema) {
		return
	}
	if schema.HasConfiguration {
		b.WriteString("HasConfiguration: true,\n")
	}
	if len(schema.ConfigurationFields) > 0 {
		fmt.Fprintf(b, "ConfigurationFields: %s,\n", objectFieldsLiteral(schema.ConfigurationFields))
	}
	if len(schema.ConfigurationDefaults) > 0 {
		b.WriteString("ConfigurationDefaults: []lang.DefaultSpec{\n")
		for _, spec := range schema.ConfigurationDefaults {
			b.WriteString(defaultLiteral(spec))
			b.WriteString(",\n")
		}
		b.WriteString("},\n")
	}
	if len(schema.ConfigurationConstraints) > 0 {
		b.WriteString("ConfigurationConstraints: []lang.ConstraintSpec{\n")
		for _, spec := range schema.ConfigurationConstraints {
			b.WriteString(specLiteral(spec))
			b.WriteString(",\n")
		}
		b.WriteString("},\n")
	}
	if schema.ConfigurationIdentity != "" {
		fmt.Fprintf(b, "ConfigurationIdentity: %q,\n", schema.ConfigurationIdentity)
	}
	if schema.ConfigurationDigest != "" {
		fmt.Fprintf(b, "ConfigurationDigest: %q,\n", schema.ConfigurationDigest)
	}
	if schema.ConfigurationEmpty {
		b.WriteString("ConfigurationEmpty: true,\n")
	}
}

func objectFieldsLiteral(fields []typecheck.ObjectField) string {
	var b strings.Builder
	b.WriteString("[]typecheck.ObjectField{\n")
	for _, field := range fields {
		b.WriteString(objectFieldLiteral(field))
		b.WriteString(",\n")
	}
	b.WriteString("}")
	return b.String()
}

func objectFieldLiteral(field typecheck.ObjectField) string {
	parts := []string{
		fmt.Sprintf("Name: %q", field.Name),
		"Type: " + typeLiteral(field.Type),
	}
	if field.Optional {
		parts = append(parts, "Optional: true")
	}
	if field.Defaulted {
		parts = append(parts, "Defaulted: true")
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func typeLiteral(t typecheck.Type) string {
	switch t.Kind {
	case typecheck.Unknown:
		return "typecheck.TUnknown()"
	case typecheck.Opaque:
		return "typecheck.TOpaque()"
	case typecheck.String:
		return "typecheck.TString()"
	case typecheck.Integer:
		return "typecheck.TInteger()"
	case typecheck.Number:
		return "typecheck.TNumber()"
	case typecheck.Boolean:
		return "typecheck.TBoolean()"
	case typecheck.Null:
		return "typecheck.TNull()"
	case typecheck.List:
		return "typecheck.TList(" + typeLiteral(elemType(t)) + ")"
	case typecheck.Map:
		return "typecheck.TMap(" + typeLiteral(elemType(t)) + ")"
	case typecheck.Object:
		return "typecheck.TObject(" + objectFieldsLiteral(t.Fields) + ")"
	case typecheck.LibraryConfig:
		info := t.LibraryConfig
		if info == nil {
			return "typecheck.TUnknown()"
		}
		return fmt.Sprintf("typecheck.TLibraryConfig(%q, %q, %q, %s)",
			info.Path, info.Identity, info.SchemaDigest, objectFieldsLiteral(t.Fields))
	case typecheck.Tuple:
		parts := make([]string, len(t.Elems))
		for i, elem := range t.Elems {
			parts[i] = typeLiteral(elem)
		}
		return "typecheck.TTuple([]typecheck.Type{" + strings.Join(parts, ", ") + "})"
	case typecheck.Optional:
		return "typecheck.TOptional(" + typeLiteral(elemType(t)) + ")"
	case typecheck.Union:
		parts := make([]string, len(t.Elems))
		for i, elem := range t.Elems {
			parts[i] = typeLiteral(elem)
		}
		return "typecheck.TUnion([]typecheck.Type{" + strings.Join(parts, ", ") + "})"
	}
	return "typecheck.TUnknown()"
}

func elemType(t typecheck.Type) typecheck.Type {
	if t.Elem == nil {
		return typecheck.TUnknown()
	}
	return *t.Elem
}

func typeSchemaLiteral(ts *runtime.TypeSchema) string {
	parts := []string{}
	if len(ts.SensitiveInputs) > 0 {
		parts = append(parts, stringSliceField("SensitiveInputs", ts.SensitiveInputs))
	}
	if len(ts.SensitiveOutputs) > 0 {
		parts = append(parts, stringSliceField("SensitiveOutputs", ts.SensitiveOutputs))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func stringSliceField(name string, values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = strconv.Quote(value)
	}
	return name + ": []string{" + strings.Join(quoted, ", ") + "}"
}

func sortedSchemaTypeKeys(types map[string]*runtime.TypeSchema) []string {
	keys := make([]string, 0, len(types))
	for typ, ts := range types {
		if ts != nil && (len(ts.SensitiveInputs) > 0 || len(ts.SensitiveOutputs) > 0) {
			keys = append(keys, typ)
		}
	}
	slices.Sort(keys)
	return keys
}

// defaultLiteral renders one spec as an element of a []lang.DefaultSpec,
// with the element type elided and empty fields omitted.
func defaultLiteral(s lang.DefaultSpec) string {
	parts := []string{fmt.Sprintf("Field: %q", s.Field)}
	if s.Value != "" {
		parts = append(parts, fmt.Sprintf("Value: %q", s.Value))
	}
	if s.Optional {
		parts = append(parts, "Optional: true")
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// specLiteral renders one spec as an element of a []lang.ConstraintSpec,
// with the element type elided and empty fields omitted.
func specLiteral(s lang.ConstraintSpec) string {
	parts := []string{fmt.Sprintf("Kind: %q", s.Kind)}
	if len(s.Fields) > 0 {
		quoted := make([]string, len(s.Fields))
		for i, f := range s.Fields {
			quoted[i] = strconv.Quote(f)
		}
		parts = append(parts, "Fields: []string{"+strings.Join(quoted, ", ")+"}")
	}
	if s.When != "" {
		parts = append(parts, fmt.Sprintf("When: %q", s.When))
	}
	if s.Require != "" {
		parts = append(parts, fmt.Sprintf("Require: %q", s.Require))
	}
	if s.Message != "" {
		parts = append(parts, fmt.Sprintf("Message: %q", s.Message))
	}
	if s.ForEach != "" {
		parts = append(parts, fmt.Sprintf("ForEach: %q", s.ForEach))
	}
	if len(s.ForEachLevels) > 0 {
		levels := make([]string, len(s.ForEachLevels))
		for i, lv := range s.ForEachLevels {
			levels[i] = fmt.Sprintf("{Name: %q, In: %q}", lv.Name, lv.In)
		}
		parts = append(parts, "ForEachLevels: []lang.ForEachSpecLevel{"+
			strings.Join(levels, ", ")+"}")
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func sortedSpecKeys[T any](m map[string][]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

var mainTemplate = template.Must(template.New("main.go").Funcs(template.FuncMap{
	"quote":             quote,
	"injectConstraints": injectConstraints,
	"injectDefaults":    injectDefaults,
	"injectSchema":      injectSchema,
}).Parse(`// Code generated by unobin. DO NOT EDIT.
package main

import (
{{if .HasLang}}	"github.com/cloudboss/unobin/pkg/lang"
{{end}}	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runner"
	"github.com/cloudboss/unobin/pkg/runtime"
{{if .HasTypecheck}}	"github.com/cloudboss/unobin/pkg/typecheck"
{{end}}{{range .GoImports}}	{{.GoIdent}} {{quote .Path}}
{{end}}{{range .UBImports}}	{{.GoIdent}} {{quote .Path}}
{{end -}}
)

var factorySource = parse.NewSourceFile(
	{{quote .FactorySourcePath}},
	{{.FactoryLineStarts}},
)

func sp(start, end int) parse.Span {
	return factorySource.Span(start, end)
}

var factoryBody = {{.FactoryBodyLiteral}}

const (
	factoryLibraryPath = {{quote .LibraryPath}}
	factoryName        = {{quote .FactoryName}}
)

// Stamped at link time via -ldflags.
var (
	factoryVersion  string
	contentRevision string
	unobinVersion   string
)

func main() {
{{if .Inject -}}
	libraries := map[string]*runtime.Library{
{{range .GoImports}}		{{quote .LocalAlias}}: runtime.LibraryWithPath(
			{{.GoIdent}}.Library(),
			{{quote .Path}},
		),
{{end}}{{range .UBImports}}		{{quote .LocalAlias}}: runtime.LibraryWithPath(
			{{.GoIdent}}.Library(),
			{{quote .Path}},
		),
{{end}}	}
{{range .ConstraintAliases}}	{{injectConstraints . $.GoConstraints}}
{{end}}{{range .DefaultAliases}}	{{injectDefaults . $.GoDefaults}}
{{end}}{{range .SchemaAliases}}	{{injectSchema . $.GoSchemas}}
{{end}}	runner.Run(runner.Info{
		FactoryName:     factoryName,
		FactoryVersion:  factoryVersion,
		ContentRevision: contentRevision,
		FactoryBody:     &factoryBody,
		LibraryPath:     factoryLibraryPath,
		Libraries:       libraries,
		UnobinVersion:   unobinVersion,
	})
{{else -}}
	runner.Run(runner.Info{
		FactoryName:     factoryName,
		FactoryVersion:  factoryVersion,
		ContentRevision: contentRevision,
		FactoryBody:     &factoryBody,
		LibraryPath:     factoryLibraryPath,
		Libraries: map[string]*runtime.Library{
{{range .GoImports}}			{{quote .LocalAlias}}: runtime.LibraryWithPath(
				{{.GoIdent}}.Library(),
				{{quote .Path}},
			),
{{end}}{{range .UBImports}}			{{quote .LocalAlias}}: runtime.LibraryWithPath(
				{{.GoIdent}}.Library(),
				{{quote .Path}},
			),
{{end}}		},
		UnobinVersion: unobinVersion,
	})
{{end -}}
}
`))
