package resolve

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// ResolveAll walks a starting file's imports and every UB library
// reachable through them, building a `Graph` and returning every error
// encountered. Pass a stable id for the starting file and a `Resolver`
// for top level imports. Local imports inside a UB library are resolved
// against that library's `fs.FS` root.
//
// The graph's node ids are: the caller-supplied id for the starting
// file; for each imported UB library, "<parent-id>/<alias>"; for each
// exported file inside a UB library, "<library-id>:<export-name>".
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

	for _, alias := range sortedAliases(refs) {
		ref := refs[alias]
		src, err := w.resolveOne(parentSrc, ref)
		if err != nil {
			w.errors = append(w.errors, fmt.Errorf("import %q: %w", alias, err))
			continue
		}
		childID := importNodeID(id, alias, ref)
		w.graph.AddEdge(id, childID)
		if !IsUBLibrary(src) {
			// Go library or some other leaf - record as a node, do not recurse.
			w.graph.AddNode(childID)
			continue
		}
		w.walkUBLibrary(childID, src)
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
				"local import %q in a UB library must stay inside the library", li.Path)
		}
		sub, err := fs.Sub(parentSrc.FS, clean)
		if err != nil {
			return nil, err
		}
		return &Source{FS: sub}, nil
	}
	return w.resolver.Resolve(ref)
}

// walkUBLibrary reads the library's manifest and recurses into each
// exported file. Errors during manifest parsing or export reads are
// recorded but do not stop the walk.
func (w *walker) walkUBLibrary(libraryID string, src *Source) {
	b, err := fs.ReadFile(src.FS, "library.ub")
	if err != nil {
		w.errors = append(w.errors,
			fmt.Errorf("library %q: read library.ub: %w", libraryID, err))
		return
	}
	manifest, err := lang.ParseSource(path.Join(libraryID, "library.ub"), b)
	if err != nil {
		w.errors = append(w.errors,
			fmt.Errorf("library %q: parse library.ub: %w", libraryID, err))
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
		exportID := libraryID + ":" + exportName
		w.graph.AddEdge(libraryID, exportID)

		body, err := fs.ReadFile(src.FS, exportPath)
		if err != nil {
			w.errors = append(w.errors,
				fmt.Errorf("library %q export %q: read %s: %w",
					libraryID, exportName, exportPath, err))
			continue
		}
		exportFile, err := lang.ParseSource(path.Join(libraryID, exportPath), body)
		if err != nil {
			w.errors = append(w.errors,
				fmt.Errorf("library %q export %q: parse: %w",
					libraryID, exportName, err))
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
