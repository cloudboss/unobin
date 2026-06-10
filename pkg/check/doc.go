// Package check runs the compile-time checks over a parsed stack:
// reference and type resolution, literal constraint evaluation, and
// @for-each nesting. The compiler runs every check; a factory
// binary's validate command re-runs the reference check on demand.
// Construction builds the stack's dependency graph once, and the
// graph is shared with whoever goes on to execute the stack.
package check
