package resolve

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"golang.org/x/mod/modfile"
)

// UBKey is the dedup key for a UB-library import. Remote imports key on
// URL, subdir, and version; the `//<subdir>` segment is included only
// when the import names a subdirectory, so root-of-repo refs read
// cleanly in cycle errors and other diagnostics. Local imports key on
// path.
func UBKey(ref ImportRef) string {
	switch r := ref.(type) {
	case *RemoteImport:
		path := r.URL
		if r.Subdir != "" {
			path += "//" + r.Subdir
		}
		return "remote:" + path + "@" + r.Version
	case *LocalImport:
		return "local:" + r.Path
	}
	return ""
}

// ResolutionKind tags how an import was resolved.
type ResolutionKind int

const (
	// ResolutionGo names a Go-library import: a remote ref whose resolved
	// source is not a UB library.
	ResolutionGo ResolutionKind = iota + 1
	// ResolutionUB names a UB-library import: a ref whose resolved source
	// has importable UB files at its root.
	ResolutionUB
)

// Resolution describes one import after the walker reaches it. For Go
// imports, Path is the canonical Go-import path (URL plus subdir when
// present) and Version is the pinned version. For UB imports,
// CanonicalKey is the dedup key (see UBKey) and visitors look up their
// per-library state by that key. SourcePath is the on-disk directory
// where the resolver fetched the import, useful for compile-time
// inspection.
type Resolution struct {
	Kind         ResolutionKind
	LocalAlias   string
	Ref          ImportRef
	Path         string
	Version      string
	CanonicalKey string
	SourcePath   string
}

// UBLibrary has everything the visitor needs about a UB library the
// first time the walker reaches it. Bodies maps node kind and composite
// name to the parsed body file; the name comes from a kind-prefixed
// filename (`<kind>-<type>.ub`) or a grammar-first composite declaration.
// BodyImports maps the same kind and name to the resolved imports declared
// by that body, in alias-sorted order so callers see a stable view across
// runs.
type UBLibrary struct {
	Bodies      map[string]map[string]*lang.File
	BodyImports map[string]map[string][]Resolution
}

// UBVisitor is implemented by callers that want to consume the walked
// import graph. The walker invokes its methods as it descends.
type UBVisitor interface {
	// OnGoImport is called for every site whose import resolves to a
	// Go library. May fire multiple times with the same path when the
	// same library is imported from several sites; visitors that need
	// uniqueness dedup themselves.
	OnGoImport(alias, path, version string) error
	// OnUBLibrary is called once per canonical key. alias is the local
	// alias of whichever site first reached the library (which matters
	// when the visitor names a directory or package after it).
	OnUBLibrary(alias, canonicalKey string, ref ImportRef, lib *UBLibrary) error
}

// WalkUB walks refs and every UB library they transitively reach,
// invoking the visitor for each import. The returned slice mirrors
// refs in resolved form, alias-sorted, so callers can build their own
// alias-to-resolution map without per-site visitor callbacks. Cycles
// through UB libraries are reported as errors.
//
// versions maps a repository URL to the version selected for it in the
// lock; every remote import is walked at its repository's selected
// version. A remote import whose repository is not in the map has no
// version and is an error: the lock must supply it.
func WalkUB(
	refs map[string]ImportRef, resolver Resolver, v UBVisitor, versions map[string]string,
) ([]Resolution, error) {
	w := &ubWalker{
		resolver:   resolver,
		visitor:    v,
		versions:   versions,
		parsed:     map[string]*UBLibrary{},
		inProgress: map[string]bool{},
	}
	return w.walkRefs(refs, "")
}

type ubWalker struct {
	resolver   Resolver
	visitor    UBVisitor
	versions   map[string]string
	parsed     map[string]*UBLibrary
	inProgress map[string]bool
}

// lockedVersion returns ref with the version the lock selected for its
// repository filled in, when the map has one. Local imports and
// repositories absent from the map are returned unchanged.
func (w *ubWalker) lockedVersion(ref ImportRef) ImportRef {
	r, ok := ref.(*RemoteImport)
	if !ok {
		return ref
	}
	v, found := w.versions[r.URL]
	if !found {
		return ref
	}
	clone := *r
	clone.Version = v
	return &clone
}

// walkRefs walks each ref in alias order. repo is the repository the
// declaring body lives in (empty at the factory root); it scopes the
// internal-visibility check.
func (w *ubWalker) walkRefs(refs map[string]ImportRef, repo string) ([]Resolution, error) {
	aliases := sortedAliases(refs)
	out := make([]Resolution, 0, len(aliases))
	for _, alias := range aliases {
		res, err := w.walkOne(alias, refs[alias], repo)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, nil
}

func (w *ubWalker) walkOne(alias string, ref ImportRef, repo string) (Resolution, error) {
	ref = w.lockedVersion(ref)
	if r, ok := ref.(*RemoteImport); ok && r.Version == "" {
		return Resolution{}, fmt.Errorf(
			"import %q: no version for %s in lock.ub; run `unobin deps sync`", alias, r.URL)
	}
	if r, ok := crossRepoInternal(repo, ref); ok {
		return Resolution{}, internalImportError(alias, r)
	}
	source, err := w.resolver.Resolve(ref)
	if err != nil {
		return Resolution{}, fmt.Errorf("import %q: %w", alias, err)
	}
	if ContainsFactorySource(source) {
		return Resolution{}, fmt.Errorf("import %q: a factory cannot be imported", alias)
	}
	if !IsUBLibrary(source) {
		return w.handleGoImport(alias, ref, source)
	}
	return w.handleUBImport(alias, ref, source, repo)
}

func (w *ubWalker) handleGoImport(
	alias string, ref ImportRef, source *Source,
) (Resolution, error) {
	r, ok := ref.(*RemoteImport)
	if !ok {
		return Resolution{}, LocalGoImportError(alias, ref.(*LocalImport).Path, source)
	}
	path := r.URL
	if r.Subdir != "" {
		path += "/" + r.Subdir
	}
	if err := w.visitor.OnGoImport(alias, path, r.Version); err != nil {
		return Resolution{}, fmt.Errorf("import %q: %w", alias, err)
	}
	return Resolution{
		Kind:       ResolutionGo,
		LocalAlias: alias,
		Ref:        ref,
		Path:       path,
		Version:    r.Version,
		SourcePath: source.Path,
	}, nil
}

// LocalGoImportError explains why a local import did not resolve to a UB
// library. When the local source is a Go module (it has a go.mod), a path
// import cannot work -- a Go library becomes a go.mod require, which needs
// a module path -- so the error shows how to import it by module path and
// replace it with the local path, naming the file each entry belongs in.
func LocalGoImportError(alias, path string, source *Source) error {
	module := localModulePath(source)
	if module == "" {
		return fmt.Errorf("import %q: %s is not a UB library", alias, path)
	}
	return fmt.Errorf("import %q: %s is a Go library (module %s), which cannot be "+
		"imported by path. Import it by its module path and replace it locally:\n"+
		"  in the .ub file: imports: { %s: '%s' }\n"+
		"  in manifest.ub:  manifest: { replace: { '%s': '%s' } }",
		alias, path, module, alias, module, module, path)
}

// localModulePath returns the module path declared in the source's go.mod,
// or an empty string when there is none.
func localModulePath(source *Source) string {
	b, err := fs.ReadFile(source.FS, "go.mod")
	if err != nil {
		return ""
	}
	return modfile.ModulePath(b)
}

func (w *ubWalker) handleUBImport(
	alias string, ref ImportRef, source *Source, repo string,
) (Resolution, error) {
	key := UBKey(ref)
	if _, alreadyParsed := w.parsed[key]; alreadyParsed {
		return Resolution{
			Kind:         ResolutionUB,
			LocalAlias:   alias,
			Ref:          ref,
			CanonicalKey: key,
		}, nil
	}
	if w.inProgress[key] {
		return Resolution{}, fmt.Errorf("import cycle through %s", key)
	}
	w.inProgress[key] = true
	defer delete(w.inProgress, key)

	lib, err := w.parseLibrary(source)
	if err != nil {
		return Resolution{}, fmt.Errorf("import %q: %w", alias, err)
	}
	lib.BodyImports = map[string]map[string][]Resolution{}
	for _, kind := range sortedKinds(lib.Bodies) {
		for _, name := range sortedBodyNames(lib.Bodies[kind]) {
			body := lib.Bodies[kind][name]
			bodyRefs, errs := ExtractImports(body)
			if len(errs) > 0 {
				return Resolution{}, errors.Join(errs...)
			}
			resols, err := w.walkRefs(bodyRefs, repoOf(repo, ref))
			if err != nil {
				return Resolution{}, fmt.Errorf(
					"import %q: composite %q: %w", alias, name, err)
			}
			if lib.BodyImports[kind] == nil {
				lib.BodyImports[kind] = map[string][]Resolution{}
			}
			lib.BodyImports[kind][name] = resols
		}
	}
	w.parsed[key] = lib
	if err := w.visitor.OnUBLibrary(alias, key, ref, lib); err != nil {
		return Resolution{}, fmt.Errorf("import %q: %w", alias, err)
	}
	return Resolution{
		Kind:         ResolutionUB,
		LocalAlias:   alias,
		Ref:          ref,
		CanonicalKey: key,
	}, nil
}

// parseLibrary reads a UB library's composite bodies from kind-prefixed body
// files and source-declared composite export files.
func (w *ubWalker) parseLibrary(source *Source) (*UBLibrary, error) {
	matches, err := fs.Glob(source.FS, "*.ub")
	if err != nil {
		return nil, err
	}
	slices.Sort(matches)
	bodies := make(map[string]map[string]*lang.File, len(matches))
	for _, filename := range matches {
		kind, typeName, ok := ubKindAndType(filename)
		b, err := readSourceFile(source, filename)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filename, err)
		}
		if !ok {
			if err := addSourceDeclaredLibraryFile(filename, b, bodies); err != nil {
				return nil, err
			}
			continue
		}
		f, err := lang.ParseSource(filename, b)
		if err != nil {
			return nil, err
		}
		f.Kind = lang.FileExportedType
		if errs := lang.ValidateFile(f); errs.Len() > 0 {
			return nil, errs.Err()
		}
		if err := addLibraryBody(typeName, kind, f, bodies); err != nil {
			return nil, err
		}
	}
	return &UBLibrary{Bodies: bodies}, nil
}

func addSourceDeclaredLibraryFile(
	filename string,
	src []byte,
	bodies map[string]map[string]*lang.File,
) error {
	f, err := lang.ParseSource(filename, src)
	if err != nil {
		return err
	}
	sf, serrs := syntax.LowerFile(f)
	if serrs.Len() > 0 {
		return fmt.Errorf(
			"library file %q must be named <resource|data|action>-<type>.ub "+
				"or contain composite declarations",
			filename)
	}
	if sf.Kind != syntax.FileLibrary || sf.Library == nil {
		return fmt.Errorf(
			"library file %q must be named <resource|data|action>-<type>.ub "+
				"or contain composite declarations",
			filename)
	}
	if verrs := syntax.ValidateFile(sf); verrs.Len() > 0 {
		return verrs.Err()
	}
	for _, export := range sf.Library.Exports {
		body := &lang.File{
			S:        export.S,
			Kind:     lang.FileExportedType,
			Path:     filename,
			Body:     syntax.RuntimeFactoryBodyObject(export.Body),
			Comments: sf.Comments,
		}
		if err := addLibraryBody(export.Name.Name, string(export.Kind), body, bodies); err != nil {
			return err
		}
	}
	return nil
}

func addLibraryBody(
	name string,
	kind string,
	body *lang.File,
	bodies map[string]map[string]*lang.File,
) error {
	if bodies[kind] == nil {
		bodies[kind] = map[string]*lang.File{}
	}
	if _, dup := bodies[kind][name]; dup {
		return fmt.Errorf("%s composite %q is declared by more than one file", kind, name)
	}
	bodies[kind][name] = body
	return nil
}

func readSourceFile(s *Source, name string) ([]byte, error) {
	f, err := s.FS.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

func sortedAliases(refs map[string]ImportRef) []string {
	out := make([]string, 0, len(refs))
	for a := range refs {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

func sortedKinds[V any](m map[string]map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func sortedBodyNames[V any](bodies map[string]V) []string {
	out := make([]string, 0, len(bodies))
	for n := range bodies {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}
