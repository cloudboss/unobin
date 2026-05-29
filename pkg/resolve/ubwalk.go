package resolve

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"

	"github.com/cloudboss/unobin/pkg/lang"
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
	// source has no kind-prefixed body files at its root.
	ResolutionGo ResolutionKind = iota + 1
	// ResolutionUB names a UB-library import: a ref whose resolved source
	// has kind-prefixed body files at its root.
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
// first time the walker reaches it. Bodies maps composite type name to
// the parsed body file; the type name comes from a kind-prefixed
// filename (`<kind>-<type>.ub`). Kinds maps the same type name to its
// kind (`resource`, `data`, or `action`). BodyImports maps the type
// name to the resolved imports declared by that body, in alias-sorted
// order so callers see a stable view across runs.
type UBLibrary struct {
	Bodies      map[string]*lang.File
	Kinds       map[string]string
	BodyImports map[string][]Resolution
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
func WalkUB(
	refs map[string]ImportRef, resolver Resolver, v UBVisitor,
) ([]Resolution, error) {
	w := &ubWalker{
		resolver:   resolver,
		visitor:    v,
		parsed:     map[string]*UBLibrary{},
		inProgress: map[string]bool{},
	}
	return w.walkRefs(refs, "")
}

type ubWalker struct {
	resolver   Resolver
	visitor    UBVisitor
	parsed     map[string]*UBLibrary
	inProgress map[string]bool
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
	if r, ok := crossRepoInternal(repo, ref); ok {
		return Resolution{}, internalImportError(alias, r)
	}
	source, err := w.resolver.Resolve(ref)
	if err != nil {
		return Resolution{}, fmt.Errorf("import %q: %w", alias, err)
	}
	if ContainsMainUB(source) {
		return Resolution{}, fmt.Errorf(
			"import %q: a factory (a directory with main.ub) cannot be imported", alias)
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
		return Resolution{}, fmt.Errorf(
			"import %q: local source is not a UB library", alias)
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
	lib.BodyImports = map[string][]Resolution{}
	for _, name := range sortedBodyNames(lib.Bodies) {
		body := lib.Bodies[name]
		bodyRefs, errs := ExtractImports(body)
		if len(errs) > 0 {
			return Resolution{}, errors.Join(errs...)
		}
		resols, err := w.walkRefs(bodyRefs, repoOf(repo, ref))
		if err != nil {
			return Resolution{}, fmt.Errorf(
				"import %q: composite %q: %w", alias, name, err)
		}
		lib.BodyImports[name] = resols
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

// parseLibrary reads a UB library's composite bodies straight from its
// directory listing: every kind-prefixed `.ub` file is one composite,
// with the type name and kind taken from the filename. There is no
// manifest. A `.ub` file whose name is not `<kind>-<type>.ub` is an
// error, as is two files naming the same type.
func (w *ubWalker) parseLibrary(source *Source) (*UBLibrary, error) {
	matches, err := fs.Glob(source.FS, "*.ub")
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	bodies := make(map[string]*lang.File, len(matches))
	kinds := make(map[string]string, len(matches))
	for _, filename := range matches {
		kind, typeName, ok := ubKindAndType(filename)
		if !ok {
			return nil, fmt.Errorf(
				"library file %q must be named <resource|data|action>-<type>.ub", filename)
		}
		if _, dup := bodies[typeName]; dup {
			return nil, fmt.Errorf(
				"composite type %q is declared by more than one file", typeName)
		}
		b, err := readSourceFile(source, filename)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filename, err)
		}
		f, err := lang.ParseSource(filename, b)
		if err != nil {
			return nil, err
		}
		f.Kind = lang.FileExportedType
		if errs := lang.ValidateFile(f); errs.Len() > 0 {
			return nil, errs.Err()
		}
		bodies[typeName] = f
		kinds[typeName] = kind
	}
	return &UBLibrary{Bodies: bodies, Kinds: kinds}, nil
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
	sort.Strings(out)
	return out
}

func sortedBodyNames(bodies map[string]*lang.File) []string {
	out := make([]string, 0, len(bodies))
	for n := range bodies {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
