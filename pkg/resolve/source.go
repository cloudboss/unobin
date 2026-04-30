package resolve

import "io/fs"

// Source is the file tree of a resolved import, rooted at the import's
// subdirectory, or the repo root when there is no subdir. For remote
// imports, Commit and Hash record the resolved git commit and a content
// hash so the lock file can pin reproducibility. Local imports leave
// both empty since their content is whatever the developer has now.
type Source struct {
	FS     fs.FS
	Commit string
	Hash   string
}

// Resolver turns an ImportRef into a Source. Implementations cover one
// kind of import each (local filesystem, remote git, etc.); callers
// dispatch by type-switching on the ref.
type Resolver interface {
	Resolve(ref ImportRef) (*Source, error)
}

// IsUBModule reports whether s carries a `module.ub` manifest at its
// root. UB modules are identified structurally by the presence of that
// file, while sources without it are Go modules.
func IsUBModule(s *Source) bool {
	if s == nil || s.FS == nil {
		return false
	}
	_, err := fs.Stat(s.FS, "module.ub")
	return err == nil
}
