// Package resolve handles import resolution and lock file management.
//
// Resolves import paths (bare URLs, with // subdirs, local ./ paths) to
// concrete sources. Detects whether each import is a UB library (it has
// kind-prefixed body files at the root) or a Go library. Detects cycles
// at resolve time and reports them as compile errors. Enforces same-repo
// imports sharing a version.
//
// Reads and writes unobin.lock - pins git commits and content hashes per
// import for reproducible compiles.
package resolve
