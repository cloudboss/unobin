package lang

import "path/filepath"

// ClassifyByFilename returns the file kind implied by the path's basename.
// `factory.ub` and legacy `main.ub` are FileFactory. `manifest.ub` and
// legacy `unobin.manifest` are FileManifest. Callers classify FileExportedType
// and FileConfig from their own context.
func ClassifyByFilename(path string) FileKind {
	switch filepath.Base(path) {
	case "factory.ub", "main.ub":
		return FileFactory
	case "manifest.ub", "unobin.manifest":
		return FileManifest
	}
	return FileUnknown
}
