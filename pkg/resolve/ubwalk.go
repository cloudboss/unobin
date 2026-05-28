package resolve

import (
	"errors"
	"fmt"
	"io"
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
	// ResolutionGo names a Go-library import: a remote ref with no
	// library.ub at the root of its resolved source.
	ResolutionGo ResolutionKind = iota + 1
	// ResolutionUB names a UB-library import: a ref whose resolved
	// source has a library.ub at the root.
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

// UBLibrary carries everything the visitor needs about a UB library the
// first time the walker reaches it. Manifest is the parsed library.ub.
// Bodies maps export name to the parsed body file. BodyImports maps
// export name to the resolved imports declared by that body, in
// alias-sorted order so callers see a stable view across runs.
type UBLibrary struct {
	Manifest    *lang.File
	Bodies      map[string]*lang.File
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
	return w.walkRefs(refs)
}

type ubWalker struct {
	resolver   Resolver
	visitor    UBVisitor
	parsed     map[string]*UBLibrary
	inProgress map[string]bool
}

func (w *ubWalker) walkRefs(refs map[string]ImportRef) ([]Resolution, error) {
	aliases := sortedAliases(refs)
	out := make([]Resolution, 0, len(aliases))
	for _, alias := range aliases {
		res, err := w.walkOne(alias, refs[alias])
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, nil
}

func (w *ubWalker) walkOne(alias string, ref ImportRef) (Resolution, error) {
	source, err := w.resolver.Resolve(ref)
	if err != nil {
		return Resolution{}, fmt.Errorf("import %q: %w", alias, err)
	}
	if !IsUBLibrary(source) {
		return w.handleGoImport(alias, ref, source)
	}
	return w.handleUBImport(alias, ref, source)
}

func (w *ubWalker) handleGoImport(
	alias string, ref ImportRef, source *Source,
) (Resolution, error) {
	r, ok := ref.(*RemoteImport)
	if !ok {
		return Resolution{}, fmt.Errorf(
			"import %q: local source has no library.ub", alias)
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
	alias string, ref ImportRef, source *Source,
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
		resols, err := w.walkRefs(bodyRefs)
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

func (w *ubWalker) parseLibrary(source *Source) (*UBLibrary, error) {
	manifestBytes, err := readSourceFile(source, "library.ub")
	if err != nil {
		return nil, fmt.Errorf("read library.ub: %w", err)
	}
	manifest, err := lang.ParseSource("library.ub", manifestBytes)
	if err != nil {
		return nil, err
	}
	manifest.Kind = lang.FileLibrary
	if errs := lang.ValidateFile(manifest); errs.Len() > 0 {
		return nil, errs.Err()
	}
	exports, err := readManifestExports(manifest)
	if err != nil {
		return nil, err
	}
	bodies := make(map[string]*lang.File, len(exports))
	for name, path := range exports {
		b, err := readSourceFile(source, path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		f, err := lang.ParseSource(path, b)
		if err != nil {
			return nil, err
		}
		f.Kind = lang.FileExportedType
		if errs := lang.ValidateFile(f); errs.Len() > 0 {
			return nil, errs.Err()
		}
		bodies[name] = f
	}
	return &UBLibrary{Manifest: manifest, Bodies: bodies}, nil
}

func readSourceFile(s *Source, name string) ([]byte, error) {
	f, err := s.FS.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

func readManifestExports(f *lang.File) (map[string]string, error) {
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "exports" {
			continue
		}
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			return nil, fmt.Errorf("`exports:` must be an object")
		}
		out := make(map[string]string, len(obj.Fields))
		for _, ef := range obj.Fields {
			if ef.Key.Kind != lang.FieldIdent || ef.Key.IsMeta() {
				continue
			}
			s, ok := ef.Value.(*lang.StringLit)
			if !ok {
				return nil, fmt.Errorf("export %q: value must be a string", ef.Key.Name)
			}
			out[ef.Key.Name] = s.Value
		}
		return out, nil
	}
	return map[string]string{}, nil
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
