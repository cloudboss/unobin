// Package core hosts the built-in actions that ship with every
// compiled factory binary. The language's functions live in the @core
// namespace, provided by the toolchain with no import; this library
// provides the actions a stack reaches under the core alias.
//
// Built-in actions:
//   - core.command - exec a process, capture stdout/stderr/exit
//   - core.http - HTTP request, return body/status
//   - core.wait-for - poll a predicate until true or timeout
//   - core.script - multi-line script (uses triple-quoted multilines)
//
// Actions implement the standard action interface: triggered (hash-based
// re-run), with @lock cross-DAG serialization, @timeout, and @sensitive
// redaction.
package core
