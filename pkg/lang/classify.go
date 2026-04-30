package lang

import "path/filepath"

// ClassifyByFilename returns the file kind implied by the path's basename.
// `stack.ub` is FileStack and `module.ub` is FileModule; anything else is
// FileUnknown. Callers classify FileExportedType (a `.ub` referenced from
// a module's `exports:` map) and FileConfig (the operator's deployment
// file, supplied by path flag) from their own context.
func ClassifyByFilename(path string) FileKind {
	switch filepath.Base(path) {
	case "stack.ub":
		return FileStack
	case "module.ub":
		return FileModule
	default:
		return FileUnknown
	}
}
