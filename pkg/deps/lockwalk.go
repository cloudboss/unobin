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

// LockFromImports builds the lock for the project rooted at rootFS. It
// walks every .ub file under the root -- a factory's main.ub, library
// bodies at the root, or libraries in subdirectories -- and through remote
// UB libraries their imports too. Each remote library becomes one lock
// entry, keyed by `repo//subdir`. Local imports are not locked and need no
// following: the walk already visits every file under the root, so a local
// library's own imports are reached directly. A library's version is its
// repository's selected version; a repository the selection does not cover
// is an error (it is imported but no floor reached it). Kind and content
// hash come from the fetched library subtree, so a Go library and a UB
// library in the same repo are recorded distinctly. A repository named in
// replace is read from its local path and never locked; a replaced UB
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

// lockFileImports reads one of the project's own .ub files and locks the
// remote libraries it imports. Local imports are skipped: the project walk
// visits every .ub file under the root, so a local library's files are
// reached on their own.
func (w *lockWalker) lockFileImports(rootFS fs.FS, path string) error {
	b, err := fs.ReadFile(rootFS, path)
	if err != nil {
		return err
	}
	f, err := lang.ParseSource(path, b)
	if err != nil {
		return err
	}
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
		r, ok := refs[alias].(*resolve.RemoteImport)
		if !ok {
			continue
		}
		if err := w.walkRemote(r); err != nil {
			return fmt.Errorf("import %q: %w", alias, err)
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

// walkReplaced handles an import whose repository the manifest replaces
// with a local path. The resolver serves it from disk; like a local
// import it is not locked (its content is whatever is on disk), but a UB
// library's own remote dependencies are still walked and locked.
func (w *lockWalker) walkReplaced(r *resolve.RemoteImport) error {
	src, err := w.resolver.Resolve(&resolve.RemoteImport{URL: r.URL, Subdir: r.Subdir})
	if err != nil {
		return err
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
