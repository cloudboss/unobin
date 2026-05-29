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
func GenerateUBLibrary(alias string,
	bodies map[string]*lang.File,
	kinds map[string]string,
	imports map[string]map[string]string) ([]byte, error) {
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
			ident := idents.identFor(imports[name][localAlias])
			entry.Libraries = append(entry.Libraries, libraryBinding{
				LocalAlias: localAlias,
				GoIdent:    ident,
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

	var buf bytes.Buffer
	data := struct {
		Alias     string
		Groups    []*compositeGroup
		GoImports []goImport
	}{
		Alias:     alias,
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
	GoIdent    string
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
	return &runtime.Library{
		Name: {{quote .Alias}},
{{range .Groups}}		{{.MapField}}: map[string]*runtime.CompositeType{
{{range .Entries}}			{{quote .Name}}: {
				Name:     {{quote .Name}},
				Kind:     {{.Symbol}},
				Body:     {{.Body}},
{{- if .Libraries}}
				Libraries: map[string]*runtime.Library{
{{range .Libraries}}					{{quote .LocalAlias}}: {{.GoIdent}}.Library(),
{{end}}				},
{{- end}}
			},
{{end}}		},
{{end}}	}
}
`))
