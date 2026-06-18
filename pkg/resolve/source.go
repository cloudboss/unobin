package resolve

import (
	"errors"
	"fmt"
	"io/fs"
	pathpkg "path"
	"slices"
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

// ContextResolver resolves an import from the package source that declared it.
type ContextResolver interface {
	ResolveFrom(ref ImportRef, parent *Source) (*Source, error)
}

// IsUBLibrary reports whether s is a UB-implemented library: a directory
// with at least one source-declared composite export and no factory source.
// Manifest and lock files do not make a package importable by themselves.
// A malformed non-metadata `.ub` file is treated as a UB library candidate
// so the library parser can return the source diagnostic.
func IsUBLibrary(s *Source) bool {
	return !ContainsFactorySource(s) && HasCompositeExports(s)
}

// HasCompositeExports reports whether s has source-declared composite exports.
func HasCompositeExports(s *Source) bool {
	if s == nil || s.FS == nil {
		return false
	}
	matches, err := librarySourceFiles(s.FS)
	if err != nil {
		return true
	}
	for _, name := range matches {
		if sourceFileMayBeLibrary(s.FS, name) {
			return true
		}
	}
	return false
}

func librarySourceFiles(fsys fs.FS) ([]string, error) {
	var paths []string
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name == "." {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			hasManifest, err := sourceDirHasManifest(fsys, name)
			if err != nil {
				return err
			}
			if hasManifest {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".ub") {
			paths = append(paths, name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	return paths, nil
}

func sourceDirHasManifest(fsys fs.FS, dir string) (bool, error) {
	manifestPath := pathpkg.Join(dir, "manifest.ub")
	b, err := fs.ReadFile(fsys, manifestPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	f, err := syntax.ParseSource(manifestPath, b)
	if err != nil {
		return true, err
	}
	if f.Kind != syntax.FileManifest || f.Manifest == nil {
		return true, fmt.Errorf("%s must declare manifest", manifestPath)
	}
	if errs := syntax.ValidateFile(f); errs.Len() > 0 {
		return true, errs.Err()
	}
	return true, nil
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
