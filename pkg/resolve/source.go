package resolve

import (
	"io/fs"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
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
// with at least one source-declared composite export and no factory source.
// Manifest and lock files do not make a package importable by themselves.
// A malformed non-metadata `.ub` file is treated as a UB library candidate
// so the library parser can return the source diagnostic.
func IsUBLibrary(s *Source) bool {
	if s == nil || s.FS == nil || ContainsFactorySource(s) {
		return false
	}
	matches, err := fs.Glob(s.FS, "*.ub")
	if err != nil {
		return false
	}
	for _, name := range matches {
		if sourceFileMayBeLibrary(s.FS, name) {
			return true
		}
	}
	return false
}

func sourceFileMayBeLibrary(fsys fs.FS, name string) bool {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return true
	}
	f, err := syntax.ParseSource(name, b)
	if err != nil {
		return !isMetadataFileName(name)
	}
	return f.Kind == syntax.FileLibrary
}

func isMetadataFileName(name string) bool {
	switch cleanSourceName(name) {
	case "manifest.ub", "lock.ub":
		return true
	default:
		return false
	}
}

func isReservedSourceFileName(name string) bool {
	switch cleanSourceName(name) {
	case "factory.ub", "manifest.ub", "lock.ub":
		return true
	default:
		return false
	}
}

func cleanSourceName(name string) string {
	return strings.TrimPrefix(name, "./")
}

// ContainsFactorySource reports whether s has a root file that declares a
// runnable factory instead of an importable library.
func ContainsFactorySource(s *Source) bool {
	if s == nil || s.FS == nil {
		return false
	}
	b, err := fs.ReadFile(s.FS, "factory.ub")
	if err != nil {
		return false
	}
	f, err := syntax.ParseSource("factory.ub", b)
	if err != nil {
		return false
	}
	return f.Kind == syntax.FileFactory && f.Factory != nil
}
