package deps

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/resolve"
)

// LockFromImports builds the lock for the factory whose source is rooted
// at rootFS. It walks the import graph from main.ub: the factory's own
// files, and through remote UB libraries their imports too. Each remote
// library becomes one lock entry, keyed by `repo//subdir`; local imports
// are followed but never locked. A library's version is its repository's
// selected version, falling back to the version pinned on the import when
// the selection does not cover that repository (a transitive dependency
// not yet on a manifest). Kind and content hash come from the fetched
// library subtree, so a Go library and a UB library in the same repo are
// recorded distinctly.
func LockFromImports(
	rootFS fs.FS, selection map[Dependency]string, resolver resolve.Resolver,
) (*Lock, error) {
	b, err := fs.ReadFile(rootFS, "main.ub")
	if err != nil {
		return nil, err
	}
	main, err := lang.ParseSource("main.ub", b)
	if err != nil {
		return nil, err
	}
	w := &lockWalker{
		resolver:   resolver,
		selection:  selection,
		lock:       NewLock(),
		inProgress: map[string]bool{},
	}
	if err := w.walkFile(main, &resolve.Source{FS: rootFS}); err != nil {
		return nil, err
	}
	if err := validateLockedDeps(w.lock); err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	return w.lock, nil
}

type lockWalker struct {
	resolver   resolve.Resolver
	selection  map[Dependency]string
	lock       *Lock
	inProgress map[string]bool
}

func (w *lockWalker) walkFile(f *lang.File, parent *resolve.Source) error {
	refs, errs := resolve.ExtractImports(f)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	aliases := make([]string, 0, len(refs))
	for a := range refs {
		aliases = append(aliases, a)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		var err error
		switch r := refs[alias].(type) {
		case *resolve.LocalImport:
			err = w.walkLocal(r, parent)
		case *resolve.RemoteImport:
			err = w.walkRemote(r)
		}
		if err != nil {
			return fmt.Errorf("import %q: %w", alias, err)
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
	version := w.selection[repo]
	if version == "" {
		version = r.Version
	}
	src, err := w.resolver.Resolve(
		&resolve.RemoteImport{URL: r.URL, Subdir: r.Subdir, Version: repo.Tag(version)})
	if err != nil {
		return err
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

func (w *lockWalker) walkBodies(src *resolve.Source) error {
	matches, err := fs.Glob(src.FS, "*.ub")
	if err != nil {
		return err
	}
	sort.Strings(matches)
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
