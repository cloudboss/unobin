package deps

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/resolve"
)

// LockFromImports builds the lock for the project rooted at rootFS. It
// walks every .ub file under the root -- legacy main.ub, grammar-first
// factory.ub, library bodies at the root, or libraries in subdirectories --
// and through remote UB libraries their imports too. Each remote library
// becomes one lock entry, keyed by `repo//subdir`. Local imports are not
// locked and need no following: the walk already visits every file under the
// root, so a local library's own imports are reached directly. A library's
// version is its repository's selected version; a repository the selection
// does not cover is an error (it is imported but no floor reached it). Kind
// and content hash come from the fetched library subtree, so a Go library and
// a UB library in the same repo are recorded distinctly. A repository named
// in replace is read from its local path and never locked; a replaced UB
// library's own remote dependencies are still walked.
func LockFromImports(
	rootFS fs.FS, selection map[Dependency]string, resolver resolve.Resolver,
	replace map[Dependency]string,
) (*Lock, error) {
	w := &lockWalker{
		resolver:   resolver,
		selection:  selection,
		replace:    replace,
		lock:       NewLock(),
		inProgress: map[string]bool{},
	}
	err := fs.WalkDir(rootFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".ub") {
			return nil
		}
		return w.lockFileImports(rootFS, path)
	})
	if err != nil {
		return nil, err
	}
	if err := validateLockedDeps(w.lock); err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	return w.lock, nil
}

// lockFileImports reads one of the project's own .ub files, locks the
// remote libraries it imports, and checks each local import. A local
// import's UB library is not locked -- the project walk visits its files
// on their own -- but a local import that points to a Go library is
// rejected, the same as at compile.
func (w *lockWalker) lockFileImports(rootFS fs.FS, path string) error {
	b, err := fs.ReadFile(rootFS, path)
	if err != nil {
		return err
	}
	f, err := lang.ParseSource(path, b)
	if err != nil {
		return err
	}
	refs, err := lockFileImportRefs(f)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if local, ok := ref.Ref.(*resolve.LocalImport); ok {
			if err := w.checkLocalImport(ref.Label, local); err != nil {
				return err
			}
			continue
		}
		r := ref.Ref.(*resolve.RemoteImport)
		if err := w.walkRemote(r); err != nil {
			return fmt.Errorf("import %q: %w", ref.Label, err)
		}
	}
	return nil
}

type lockWalker struct {
	resolver   resolve.Resolver
	selection  map[Dependency]string
	replace    map[Dependency]string
	lock       *Lock
	inProgress map[string]bool
}

type lockImportRef struct {
	Label string
	Ref   resolve.ImportRef
}

func lockFileImportRefs(f *lang.File) ([]lockImportRef, error) {
	refs, errs := resolve.ExtractImports(f)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	out := make([]lockImportRef, 0, len(refs))
	aliases := make([]string, 0, len(refs))
	for a := range refs {
		aliases = append(aliases, a)
	}
	slices.Sort(aliases)
	for _, alias := range aliases {
		out = append(out, lockImportRef{Label: alias, Ref: refs[alias]})
	}
	if !hasSourceDeclaredImports(f) {
		return out, nil
	}
	srefs, err := extractSyntaxImportRefs(f)
	if err != nil {
		return nil, err
	}
	for _, ref := range srefs {
		out = append(out, lockImportRef{
			Label: syntaxImportLabel(ref),
			Ref:   ref.Ref,
		})
	}
	return out, nil
}

func syntaxImportLabel(ref resolve.SyntaxImport) string {
	if ref.Scope == "" {
		return ref.Alias
	}
	return ref.Scope + "." + ref.Alias
}

func (w *lockWalker) walkFile(f *lang.File, parent *resolve.Source) error {
	refs, err := lockFileImportRefs(f)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		var err error
		switch r := ref.Ref.(type) {
		case *resolve.LocalImport:
			err = w.walkLocal(r, parent)
		case *resolve.RemoteImport:
			err = w.walkRemote(r)
		}
		if err != nil {
			return fmt.Errorf("import %q: %w", ref.Label, err)
		}
	}
	return nil
}

func (w *lockWalker) walkLocal(r *resolve.LocalImport, parent *resolve.Source) error {
	clean := strings.TrimPrefix(r.Path, "./")
	if clean == "" || strings.HasPrefix(clean, "../") || strings.HasPrefix(r.Path, "/") {
		return fmt.Errorf("local import %q must stay inside the project", r.Path)
	}
	sub, err := fs.Sub(parent.FS, clean)
	if err != nil {
		return err
	}
	return w.walkBodies(&resolve.Source{FS: sub})
}

func (w *lockWalker) walkRemote(r *resolve.RemoteImport) error {
	if _, replaced := w.replace[Dependency{URL: r.URL}]; replaced {
		return w.walkReplaced(r)
	}
	id := r.URL
	if r.Subdir != "" {
		id += "//" + r.Subdir
	}
	if _, done := w.lock.Deps[id]; done {
		return nil
	}
	if w.inProgress[id] {
		return fmt.Errorf("import cycle through %s", id)
	}
	w.inProgress[id] = true
	defer delete(w.inProgress, id)

	repo := Dependency{URL: r.URL}
	version, ok := w.selection[repo]
	if !ok {
		return fmt.Errorf(
			"%s is imported but has no version floor in %s; "+
				"add one with `unobin deps get %s@<version>`",
			r.URL, ManifestFileName, r.URL)
	}
	src, err := w.resolver.Resolve(
		&resolve.RemoteImport{URL: r.URL, Subdir: r.Subdir, Version: repo.Tag(version)})
	if err != nil {
		return err
	}
	if resolve.ContainsFactorySource(src) {
		return fmt.Errorf("a factory cannot be imported")
	}
	kind := LockKindGo
	if resolve.IsUBLibrary(src) {
		kind = LockKindUB
	}
	entry := &LockedDep{
		Kind:    kind,
		Version: version,
		Commit:  src.Commit,
		Hash:    src.Hash,
	}
	if kind == LockKindUB {
		if err := w.walkBodies(src); err != nil {
			return err
		}
	}
	// Recorded after recursing, so a back-edge into a library still being
	// walked is caught as a cycle rather than treated as already done.
	w.lock.Deps[id] = entry
	return nil
}

// checkLocalImport resolves a local import and rejects it when it points
// to a Go library, which cannot be imported by path. A UB library is fine:
// the project walk visits its files directly, so nothing more is recorded.
func (w *lockWalker) checkLocalImport(alias string, r *resolve.LocalImport) error {
	src, err := w.resolver.Resolve(r)
	if err != nil {
		return fmt.Errorf("import %q: %w", alias, err)
	}
	if resolve.ContainsFactorySource(src) {
		return fmt.Errorf("import %q: a factory cannot be imported", alias)
	}
	if !resolve.IsUBLibrary(src) {
		return resolve.LocalGoImportError(alias, r.Path, src)
	}
	return nil
}

// walkReplaced handles an import whose repository the manifest replaces
// with a local path. The resolver serves it from disk; like a local
// import it is not locked (its content is whatever is on disk), but a UB
// library's own remote dependencies are still walked and locked.
func (w *lockWalker) walkReplaced(r *resolve.RemoteImport) error {
	src, err := w.resolver.Resolve(&resolve.RemoteImport{URL: r.URL, Subdir: r.Subdir})
	if err != nil {
		return err
	}
	if resolve.ContainsFactorySource(src) {
		return fmt.Errorf("a factory cannot be imported")
	}
	if resolve.IsUBLibrary(src) {
		return w.walkBodies(src)
	}
	return nil
}

func (w *lockWalker) walkBodies(src *resolve.Source) error {
	matches, err := fs.Glob(src.FS, "*.ub")
	if err != nil {
		return err
	}
	slices.Sort(matches)
	for _, name := range matches {
		b, err := fs.ReadFile(src.FS, name)
		if err != nil {
			return err
		}
		f, err := lang.ParseSource(name, b)
		if err != nil {
			return err
		}
		if err := w.walkFile(f, src); err != nil {
			return err
		}
	}
	return nil
}
