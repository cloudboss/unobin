package resolve

import (
	"fmt"
	"strings"
)

// hasInternalSegment reports whether subdir has a path element named
// "internal". unobin adopts Go's convention verbatim: a library under
// such a directory is private to its own repository.
func hasInternalSegment(subdir string) bool {
	if subdir == "" {
		return false
	}
	for seg := range strings.SplitSeq(subdir, "/") {
		if seg == "internal" {
			return true
		}
	}
	return false
}

// crossRepoInternal reports whether importing ref from a body that lives
// in repository fromRepo reaches an internal library across a repository
// boundary, returning the offending remote import when it does. A local
// import never leaves the current repository, so it is never a crossing;
// fromRepo is the empty string at the factory root and throughout the
// developer's local tree, which shares no repository with a remote import.
func crossRepoInternal(fromRepo string, ref ImportRef) (*RemoteImport, bool) {
	r, ok := ref.(*RemoteImport)
	if !ok || !hasInternalSegment(r.Subdir) {
		return nil, false
	}
	if r.URL == fromRepo {
		return nil, false
	}
	return r, true
}

// repoOf returns the repository a ref's target belongs to: a remote
// import's URL, or fromRepo carried through for a local import, since a
// local import stays within the repository of the body that declared it.
func repoOf(fromRepo string, ref ImportRef) string {
	if r, ok := ref.(*RemoteImport); ok {
		return r.URL
	}
	return fromRepo
}

// internalImportError names a refused cross-repository import of a library
// that lives under an internal directory.
func internalImportError(alias string, r *RemoteImport) error {
	return fmt.Errorf(
		"import %q: %s//%s is internal to %s and cannot be imported from another repository",
		alias, r.URL, r.Subdir, r.URL)
}
