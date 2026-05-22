// Package core hosts built-in actions and primitive types that ship with
// every stack binary.
//
// Built-in actions:
//   - core.command - exec a process, capture stdout/stderr/exit
//   - core.http - HTTP request, return body/status
//   - core.wait-for - poll a predicate until true or timeout
//   - core.script - multi-line script (uses triple-quoted multilines)
//
// Actions implement the standard action interface: triggered (hash-based
// re-run), with @lock cross-DAG serialization, @timeout, @sensitive
// redaction, and @on-completion hook semantics.
package core
