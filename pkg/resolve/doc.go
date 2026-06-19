// Package resolve handles import resolution.
//
// Resolves import paths (bare URLs, with // subdirs, local ./ paths) to
// concrete sources. Detects whether each import is a UB library (it has
// UB files at the root) or a Go library. Detects cycles at resolve time
// and reports them as compile errors. Enforces same-repo imports sharing
// a version.
package resolve
