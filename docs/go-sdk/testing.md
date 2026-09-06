# Testing

Test Go libraries at the method level and through Unobin consumers.

## Method tests

Call resource, data source, action, and function methods directly. Use temporary directories and fake clients so tests do not read or write user configuration.

For resources, cover:

- `Create` from empty state.
- `Read` after create.
- `Update` with prior inputs and outputs.
- `Delete` when the external object exists and when it is already absent.
- Definition validation, typed replacement rules, and schema migrations.

## Registration tests

Construct `Library()` in a test and assert the registrations you expect:

```go
lib := Library()
require.Contains(t, lib.Resources, "file")
require.Contains(t, lib.Actions, "echo")
```

## Consumer fixtures

Add small `.ub` fixtures that import the library and compile them with the factory. This checks the schema the compiler reads from source, not just the Go method contracts.

Use the public `github.com/cloudboss/unobin/pkg/e2etest` package:

```go
func TestConsumers(t *testing.T) {
    e2etest.RunCompiledCases(t, "testdata/ub/valid",
        e2etest.WithGoModule("example.com/my-library", "."),
    )
}
```

Each case contains a `case.json`, `.ub` source files, and expected command output
or files. The harness compiles the factory directly using the caller's Unobin
dependency and the local library. It handles stack pinning, command execution,
and comparisons of output, files, plans, and state. Use `WithEnv` for values such
as a local HTTP test server URL. See the
[framework README](https://github.com/cloudboss/unobin/blob/replacement-constructs/pkg/e2etest/README.md)
for the case format and options.

Run `go test -short ./...` for fast checks; cases with `"build": true` are skipped.
Run `go test ./...` for the full suite, including compiled consumers. Dependencies
and toolchains may need downloading on the first build, and subsequent runs reuse
the configured caches. Checks that deliberately download into an empty cache
belong in a separate release check.

## External services

When a library talks to an external service, inject a fake client. Avoid tests that depend on ambient credentials, user config files, or real network state.
