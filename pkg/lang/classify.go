package lang

import "path/filepath"

// ClassifyByFilename returns the file kind implied by the path's basename.
// `factory.ub` is FileFactory and `library.ub` is FileLibrary; anything else is
// FileUnknown. Callers classify FileExportedType (a `.ub` referenced from
// a library's `exports:` map) and FileConfig (the operator's stack config
// file, supplied by path flag) from their own context.
func ClassifyByFilename(path string) FileKind {
	switch filepath.Base(path) {
	case "factory.ub":
		return FileFactory
	case "library.ub":
		return FileLibrary
	default:
		return FileUnknown
	}
}
