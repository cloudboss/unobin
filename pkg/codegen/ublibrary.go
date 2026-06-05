package codegen

import (
	"bytes"
	"fmt"
	"go/format"
	"path"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/cloudboss/unobin/pkg/lang"
)

// GoLibrarySpecs holds one Go library's compile-extracted spec data,
// keyed by "<kind>.<type>" the way runtime.Library stores it. The dev
// CLI gathers it from the library's source; codegen embeds it in
// generated code so the runtime can look it up at plan and apply.
type GoLibrarySpecs struct {
	Constraints map[string][]lang.ConstraintSpec
	Defaults    map[string][]lang.DefaultSpec
}

// Empty reports whether the specs hold no data at all.
func (s GoLibrarySpecs) Empty() bool {
	return len(s.Constraints) == 0 && len(s.Defaults) == 0
}

// GenerateUBLibrary produces the Go source for a UB library's
// generated package. The package's name is alias; it exports a
// `Library()` function returning a `*runtime.Library` whose composites
// are split into one map per kind (ResourceComposites, DataComposites,
// ActionComposites). bodies and kinds are both keyed by composite type
// name (derived from each file's `<kind>-<type>.ub` name); kinds gives
// the kind.
//
// imports maps each composite's type name to its resolved import table:
// the composite's body's own imports block, with each declared alias
// mapped to the Go import path of the package that supplies it. The
// generated source emits one Go-level import per unique path and
// renders a per-composite `Libraries` map binding each composite-local
// alias to the corresponding package's `Library()`. Pass nil or an
// empty map when a composite has no imports.
//
// goSpecs maps a Go import path to the specs its types declare. A
// bound library with specs is constructed once in `Library()`, has its
// Constraints and Defaults attached, and every binding of that path
// shares the instance, so a composite-internal node resolves the same
// spec data a root import of the library would.
func GenerateUBLibrary(alias string,
	bodies map[string]*lang.File,
	kinds map[string]string,
	imports map[string]map[string]string,
	goSpecs map[string]GoLibrarySpecs) ([]byte, error) {
	if alias == "" {
		return nil, fmt.Errorf("ublibrary: alias is required")
	}

	idents := newIdentTable()
	names := make([]string, 0, len(bodies))
	for name := range bodies {
		names = append(names, name)
	}
	sort.Strings(names)

	groups := map[string]*compositeGroup{}
	for _, c := range compositeKinds {
		groups[c.kind] = &compositeGroup{MapField: c.mapField, Symbol: c.symbol}
	}
	for _, name := range names {
		kind := kinds[name]
		group, ok := groups[kind]
		if !ok {
			return nil, fmt.Errorf("ublibrary %q: composite %q has unknown kind %q",
				alias, name, kind)
		}
		encoded, err := EncodeNode(bodies[name])
		if err != nil {
			return nil, fmt.Errorf("ublibrary %q: encode %q body: %w", alias, name, err)
		}
		entry := compositeEntry{Name: name, Symbol: group.Symbol, Body: encoded}
		for _, localAlias := range sortedAliases(imports[name]) {
			p := imports[name][localAlias]
			entry.Libraries = append(entry.Libraries, libraryBinding{
				LocalAlias: localAlias,
				Path:       p,
				GoIdent:    idents.identFor(p),
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

	specVars, varOf := specVarsFor(idents, goSpecs)
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
		Alias     string
		SpecVars  []specVar
		Groups    []*compositeGroup
		GoImports []goImport
	}{
		Alias:     alias,
		SpecVars:  specVars,
		Groups:    orderedGroups,
		GoImports: idents.imports(),
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
	{"data", "DataComposites", "runtime.NodeData"},
	{"action", "ActionComposites", "runtime.NodeAction"},
}

type compositeGroup struct {
	MapField string
	Symbol   string
	Entries  []compositeEntry
}

type compositeEntry struct {
	Name      string
	Symbol    string
	Body      string
	Libraries []libraryBinding
}

type libraryBinding struct {
	LocalAlias string
	Path       string
	GoIdent    string
	// Value is the expression the binding renders: the shared spec
	// variable when the path declares specs, otherwise an inline
	// `<ident>.Library()` call.
	Value string
}

// specVar is one local variable the generated Library() declares for a
// Go library whose types declare specs: the library is constructed
// once, the rendered assignments attach its Constraints and Defaults,
// and every binding of the path shares the variable.
type specVar struct {
	Name        string
	GoIdent     string
	Constraints string
	Defaults    string
}

// specVarsFor returns the spec variables for every bound import path
// with declared specs, ordered by import path, plus the path-to-name
// map bindings resolve against. The variable name derives from the
// import ident, so it stays unique and never collides with a package
// name or keyword.
func specVarsFor(
	idents *identTable, goSpecs map[string]GoLibrarySpecs,
) ([]specVar, map[string]string) {
	paths := append([]string(nil), idents.order...)
	sort.Strings(paths)
	vars := make([]specVar, 0, len(goSpecs))
	varOf := make(map[string]string, len(goSpecs))
	for _, p := range paths {
		specs := goSpecs[p]
		if specs.Empty() {
			continue
		}
		ident := idents.byPath[p]
		name := strings.TrimPrefix(ident, "lib_") + "Lib"
		v := specVar{Name: name, GoIdent: ident}
		if len(specs.Constraints) > 0 {
			v.Constraints = constraintsAssign(name, specs.Constraints)
		}
		if len(specs.Defaults) > 0 {
			v.Defaults = defaultsAssign(name, specs.Defaults)
		}
		vars = append(vars, v)
		varOf[p] = name
	}
	return vars, varOf
}

type goImport struct {
	GoIdent string
	Path    string
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

func (t *identTable) imports() []goImport {
	out := make([]goImport, 0, len(t.order))
	paths := append([]string(nil), t.order...)
	sort.Strings(paths)
	for _, p := range paths {
		out = append(out, goImport{GoIdent: t.byPath[p], Path: p})
	}
	return out
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
	return b.String()
}

func sortedAliases(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var ubLibraryTemplate = template.Must(template.New("ublibrary.go").Funcs(template.FuncMap{
	"quote": strconv.Quote,
}).Parse(`// Code generated by unobin. DO NOT EDIT.
package {{.Alias}}

import (
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
{{range .GoImports}}	{{.GoIdent}} {{quote .Path}}
{{end}})

func Library() *runtime.Library {
{{range .SpecVars}}	{{.Name}} := {{.GoIdent}}.Library()
{{- if .Constraints}}
	{{.Constraints}}
{{- end}}
{{- if .Defaults}}
	{{.Defaults}}
{{- end}}
{{end}}	return &runtime.Library{
		Name: {{quote .Alias}},
{{range .Groups}}		{{.MapField}}: map[string]*runtime.CompositeType{
{{range .Entries}}			{{quote .Name}}: {
				Name:     {{quote .Name}},
				Kind:     {{.Symbol}},
				Body:     {{.Body}},
{{- if .Libraries}}
				Libraries: map[string]*runtime.Library{
{{range .Libraries}}					{{quote .LocalAlias}}: {{.Value}},
{{end}}				},
{{- end}}
			},
{{end}}		},
{{end}}	}
}
`))
