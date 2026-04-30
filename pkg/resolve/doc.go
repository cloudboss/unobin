// Package resolve handles import resolution and lock file management.
//
// Resolves import paths (bare URLs, with // subdirs, local ./ paths) to
// concrete sources. Detects whether each import is a UB module (presence of
// module.ub at the root) or a Go module. Detects cycles at resolve time and
// surfaces them as compile errors. Enforces same-repo imports sharing a
// version.
//
// Reads and writes unobin.lock - pins git commits and content hashes per
// import for reproducible compiles.
package resolve
