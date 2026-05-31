// Package core hosts the built-in actions, functions, state backend, and
// encrypters that ship with every compiled factory binary.
//
// Built-in actions:
//   - core.command - exec a process, capture stdout/stderr/exit
//   - core.http - HTTP request, return body/status
//   - core.wait-for - poll a predicate until true or timeout
//   - core.script - multi-line script (uses triple-quoted multilines)
//
// Built-in functions: core.format, core.b64-encode, core.b64-decode,
// core.range, and core.length. State backend core.local writes snapshots to
// the local filesystem; the core.env-key and core.noop encrypters cover
// encrypted and plaintext state.
//
// Actions implement the standard action interface: triggered (hash-based
// re-run), with @lock cross-DAG serialization, @timeout, and @sensitive
// redaction.
package core
