package resolve

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
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
		if ContainsMainUB(src) {
			w.errors = append(w.errors, fmt.Errorf(
				"import %q: a factory (a directory with main.ub) cannot be imported", alias))
			continue
		}
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

// walkUBLibrary reads the library's composite bodies from its directory
// listing: each category-prefixed `.ub` file is one composite, named by
// its filename. Errors during the scan or a body read are recorded but
// do not stop the walk.
func (w *walker) walkUBLibrary(libraryID string, src *Source) {
	matches, err := fs.Glob(src.FS, "*.ub")
	if err != nil {
		w.errors = append(w.errors, fmt.Errorf("library %q: %w", libraryID, err))
		return
	}
	sort.Strings(matches)
	for _, filename := range matches {
		_, typeName, ok := ubCategoryAndType(filename)
		if !ok {
			w.errors = append(w.errors, fmt.Errorf(
				"library %q: file %q must be named <resource|data|action>-<type>.ub",
				libraryID, filename))
			continue
		}
		exportID := libraryID + ":" + typeName
		w.graph.AddEdge(libraryID, exportID)

		body, err := fs.ReadFile(src.FS, filename)
		if err != nil {
			w.errors = append(w.errors,
				fmt.Errorf("library %q type %q: read %s: %w",
					libraryID, typeName, filename, err))
			continue
		}
		exportFile, err := lang.ParseSource(path.Join(libraryID, filename), body)
		if err != nil {
			w.errors = append(w.errors,
				fmt.Errorf("library %q type %q: parse: %w",
					libraryID, typeName, err))
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
