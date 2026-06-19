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
)

// Input bundles everything codegen needs to produce a factory binary's
// `main.go`. Body is the literal factory source the binary embeds and
// parses on each invocation. LibraryPath is the binary's library-path
// identity, the same form Go libraries use; the operator's stack file
// asserts the same value under `factory.pin.library-path` and plan,
// refresh, and validate refuse on mismatch. An empty LibraryPath disables that
// identity check. The version and content-revision are not generated
// here; compile stamps them into the built binary with -ldflags so the
// generated source stays a pure function of the factory content.
// GoImports maps each Go-library alias the source uses to the Go import
// path that supplies it (e.g.,
// `"std" -> "github.com/cloudboss/unobin-library-std"`).
// GoModules maps each required Go module path to the selected version for
// go.mod. A Go package import below a module appears only in GoImports.
// UBImports maps each UB-library alias to the local Go import path of
// the package that compile generated for it (typically
// `<factory-name>/internal/<alias>`).
type Input struct {
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
	data := struct {
		Body              string
		LibraryPath       string
		FactoryName       string
		GoImports         []aliasImport
		UBImports         []aliasImport
		ConstraintAliases []string
		GoConstraints     map[string]map[string][]lang.ConstraintSpec
		DefaultAliases    []string
		GoDefaults        map[string]map[string][]lang.DefaultSpec
		Inject            bool
	}{
		Body:              in.Body,
		LibraryPath:       in.LibraryPath,
		FactoryName:       in.FactoryName,
		GoImports:         goImports,
		UBImports:         ubImports,
		ConstraintAliases: constraintAliases,
		GoConstraints:     in.GoConstraints,
		DefaultAliases:    defaultAliases,
		GoDefaults:        in.GoDefaults,
		Inject:            len(constraintAliases)+len(defaultAliases) > 0,
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

// quote renders a string as a Go double quoted literal. Used by the
// template to embed the factory source verbatim.
func quote(s string) string {
	return strconv.Quote(s)
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
}).Parse(`// Code generated by unobin. DO NOT EDIT.
package main

import (
{{if .Inject}}	"github.com/cloudboss/unobin/pkg/lang"
{{end}}	"github.com/cloudboss/unobin/pkg/runner"
	"github.com/cloudboss/unobin/pkg/runtime"
{{range .GoImports}}	{{.GoIdent}} {{quote .Path}}
{{end}}{{range .UBImports}}	{{.GoIdent}} {{quote .Path}}
{{end -}}
)

const (
	factoryBody        = {{quote .Body}}
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
{{range .GoImports}}		{{quote .LocalAlias}}: {{.GoIdent}}.Library(),
{{end}}{{range .UBImports}}		{{quote .LocalAlias}}: {{.GoIdent}}.Library(),
{{end}}	}
{{range .ConstraintAliases}}	{{injectConstraints . $.GoConstraints}}
{{end}}{{range .DefaultAliases}}	{{injectDefaults . $.GoDefaults}}
{{end}}	runner.Run(runner.Info{
		FactoryName:     factoryName,
		FactoryVersion:  factoryVersion,
		ContentRevision: contentRevision,
		FactoryBody:     factoryBody,
		LibraryPath:     factoryLibraryPath,
		Libraries:       libraries,
		UnobinVersion:   unobinVersion,
	})
{{else -}}
	runner.Run(runner.Info{
		FactoryName:     factoryName,
		FactoryVersion:  factoryVersion,
		ContentRevision: contentRevision,
		FactoryBody:     factoryBody,
		LibraryPath:     factoryLibraryPath,
		Libraries: map[string]*runtime.Library{
{{range .GoImports}}			{{quote .LocalAlias}}: {{.GoIdent}}.Library(),
{{end}}{{range .UBImports}}			{{quote .LocalAlias}}: {{.GoIdent}}.Library(),
{{end}}		},
		UnobinVersion: unobinVersion,
	})
{{end -}}
}
`))
