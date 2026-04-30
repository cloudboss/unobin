package resolve

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// ResolveAll walks a starting file's imports and every UB module
// reachable through them, building a `Graph` and returning every error
// encountered. Pass a stable id for the starting file and a `Resolver`
// for top level imports. Local imports inside a UB module are resolved
// against that module's `fs.FS` root.
//
// The graph's node ids are: the caller-supplied id for the starting
// file; for each imported UB module, "<parent-id>/<alias>"; for each
// exported file inside a UB module, "<module-id>:<export-name>".
func ResolveAll(id string, f *lang.File, resolver Resolver) (*Graph, []error) {
	w := &walker{
		resolver: resolver,
		graph:    NewGraph(),
		visited:  make(map[string]bool),
	}
	w.walkFile(id, nil, f)
	return w.graph, w.errors
}

type walker struct {
	resolver Resolver
	graph    *Graph
	visited  map[string]bool
	errors   []error
}

// walkFile processes a parsed file. parentSrc is the `Source` the file
// came from. Pass nil for the starting file, which uses the external
// Resolver instead.
func (w *walker) walkFile(id string, parentSrc *Source, f *lang.File) {
	if w.visited[id] {
		return
	}
	w.visited[id] = true
	w.graph.AddNode(id)

	refs, errs := ExtractImports(f)
	w.errors = append(w.errors, errs...)
	if len(refs) == 0 {
		return
	}
	w.errors = append(w.errors, CheckSameRepoVersions(refs)...)

	for _, alias := range sortedKeys(refs) {
		ref := refs[alias]
		src, err := w.resolveOne(parentSrc, ref)
		if err != nil {
			w.errors = append(w.errors, fmt.Errorf("import %q: %w", alias, err))
			continue
		}
		childID := importNodeID(id, alias, ref)
		w.graph.AddEdge(id, childID)
		if !IsUBModule(src) {
			// Go module or some other leaf - record as a node, do not recurse.
			w.graph.AddNode(childID)
			continue
		}
		w.walkUBModule(childID, src)
	}
}

// resolveOne picks the right resolver for a ref: an inside-a-Source
// local import resolves against the parent Source's `fs.FS`. Everything
// else goes through the external Resolver.
func (w *walker) resolveOne(parentSrc *Source, ref ImportRef) (*Source, error) {
	if li, ok := ref.(*LocalImport); ok && parentSrc != nil {
		clean := strings.TrimPrefix(li.Path, "./")
		if clean == "" || strings.HasPrefix(clean, "../") || strings.HasPrefix(li.Path, "/") {
			return nil, fmt.Errorf(
				"local import %q in a UB module must stay inside the module", li.Path)
		}
		sub, err := fs.Sub(parentSrc.FS, clean)
		if err != nil {
			return nil, err
		}
		return &Source{FS: sub}, nil
	}
	return w.resolver.Resolve(ref)
}

// walkUBModule reads the module's manifest and recurses into each
// exported file. Errors during manifest parsing or export reads are
// recorded but do not stop the walk.
func (w *walker) walkUBModule(moduleID string, src *Source) {
	b, err := fs.ReadFile(src.FS, "module.ub")
	if err != nil {
		w.errors = append(w.errors,
			fmt.Errorf("module %q: read module.ub: %w", moduleID, err))
		return
	}
	manifest, err := lang.ParseSource(path.Join(moduleID, "module.ub"), b)
	if err != nil {
		w.errors = append(w.errors,
			fmt.Errorf("module %q: parse module.ub: %w", moduleID, err))
		return
	}
	exports := topLevelObject(manifest, "exports")
	if exports == nil {
		return
	}
	for _, fld := range exports.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		s, ok := fld.Value.(*lang.StringLit)
		if !ok {
			continue
		}
		exportName := fld.Key.Name
		exportPath := s.Value
		exportID := moduleID + ":" + exportName
		w.graph.AddEdge(moduleID, exportID)

		body, err := fs.ReadFile(src.FS, exportPath)
		if err != nil {
			w.errors = append(w.errors,
				fmt.Errorf("module %q export %q: read %s: %w",
					moduleID, exportName, exportPath, err))
			continue
		}
		exportFile, err := lang.ParseSource(path.Join(moduleID, exportPath), body)
		if err != nil {
			w.errors = append(w.errors,
				fmt.Errorf("module %q export %q: parse: %w",
					moduleID, exportName, err))
			continue
		}
		w.walkFile(exportID, src, exportFile)
	}
}

func importNodeID(parentID, alias string, ref ImportRef) string {
	switch r := ref.(type) {
	case *LocalImport:
		return parentID + "/" + alias
	case *RemoteImport:
		s := r.URL + "@" + r.Version
		if r.Subdir != "" {
			s += "/" + r.Subdir
		}
		return s
	default:
		return parentID + "/" + alias
	}
}

func sortedKeys(m map[string]ImportRef) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort - the slice is short and stdlib's sort import
	// would dwarf the savings.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
