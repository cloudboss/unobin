package resolve

import (
	"io/fs"
	"strings"
)

// Source is the file tree of a resolved import, rooted at the import's
// subdirectory, or the repo root when there is no subdir. For remote
// imports, Commit and Hash record the resolved git commit and a content
// hash so the lock file can pin reproducibility. Local imports leave
// both empty since their content is whatever the developer has now.
// Path is the on-disk directory the source was fetched into, which the
// dev CLI uses for compile-time inspection of Go-library source.
type Source struct {
	FS     fs.FS
	Path   string
	Commit string
	Hash   string
}

// Resolver turns an ImportRef into a Source. Implementations cover one
// kind of import each (local filesystem, remote git, etc.); callers
// dispatch by type-switching on the ref.
type Resolver interface {
	Resolve(ref ImportRef) (*Source, error)
}

// IsUBLibrary reports whether s is a UB-implemented library: a directory
// holding at least one `.ub` file and no factory source. Library files can
// be legacy kind-prefixed bodies (`resource-*.ub`, `data-*.ub`,
// `action-*.ub`) or grammar-first composite declarations. A bad `.ub` file
// is caught when the library is parsed, not here, so the author gets a clear
// error rather than having the whole directory silently treated as a Go
// library. Sources with no `.ub` files are Go libraries.
func IsUBLibrary(s *Source) bool {
	if s == nil || s.FS == nil || ContainsFactorySource(s) {
		return false
	}
	matches, err := fs.Glob(s.FS, "*.ub")
	return err == nil && len(matches) > 0
}

// ContainsFactorySource reports whether s has a root file that marks a
// runnable factory instead of an importable library.
func ContainsFactorySource(s *Source) bool {
	return ContainsMainUB(s) || containsRootFile(s, "factory.ub")
}

// ContainsMainUB reports whether s has a `main.ub` at its root, which
// marks the directory as a factory: runnable and not importable.
func ContainsMainUB(s *Source) bool {
	return containsRootFile(s, "main.ub")
}

func containsRootFile(s *Source, name string) bool {
	if s == nil || s.FS == nil {
		return false
	}
	_, err := fs.Stat(s.FS, name)
	return err == nil
}

// ubKindAndType splits a kind-prefixed body filename into its kind
// (`resource`, `data`, or `action`) and the type name. It reports
// ok=false for any name that is not `<kind>-<type>.ub`.
func ubKindAndType(filename string) (kind, typeName string, ok bool) {
	base, found := strings.CutSuffix(filename, ".ub")
	if !found {
		return "", "", false
	}
	prefix, rest, found := strings.Cut(base, "-")
	if !found || rest == "" {
		return "", "", false
	}
	switch prefix {
	case "resource", "data", "action":
		return prefix, rest, true
	}
	return "", "", false
}
