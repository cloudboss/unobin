# Testing

Test Go libraries at the method level and through Unobin consumers.

## Method tests

Call resource, data source, action, and function methods directly. Use temporary directories and fake clients so tests do not read or write user configuration.

For resources, cover:

- `Create` from empty state.
- `Read` after create.
- `Update` with prior inputs and outputs.
- `Delete` when the external object exists and when it is already absent.
- `ReplaceFields` and `SchemaVersion`.

## Registration tests

Construct `Library()` in a test and assert the registrations you expect:

```go
lib := Library()
require.Contains(t, lib.Resources, "file")
require.Contains(t, lib.Actions, "echo")
```

## Consumer fixtures

Add small `.ub` fixtures that import the library and compile them with the factory. This checks the schema the compiler reads from source, not just the Go method contracts.

## External services

When a library talks to an external service, inject a fake client. Avoid tests that depend on ambient credentials, user config files, or real network state.
