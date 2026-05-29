package lang

import "path/filepath"

// ClassifyByFilename returns the file kind implied by the path's basename.
// `main.ub` is FileFactory; anything else is FileUnknown. Callers classify
// FileExportedType (a kind-prefixed `<kind>-<type>.ub` inside a
// library) and FileConfig (the operator's stack config file, supplied by
// path flag) from their own context.
func ClassifyByFilename(path string) FileKind {
	if filepath.Base(path) == "main.ub" {
		return FileFactory
	}
	return FileUnknown
}
