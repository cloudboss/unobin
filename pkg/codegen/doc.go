// Package codegen generates Go source from a resolved AST.
//
// Uses dst (formatting-preserving Go AST) - preserved from the POC. Produces
// the main.go that statically links all imports (Go modules and UB modules).
// Embeds the source body and module-path so the binary self-reports its
// identity; the version and content-revision are stamped in at link time. UB
// modules are inlined as expanded sub-DAGs (composite types decompose into
// internal resources at code-gen time).
//
// Output is a complete Go module that the Go toolchain compiles into the
// stack binary.
package codegen
