# Go SDK overview

The Go SDK is for library authors.

A Go library must export a `Library` function that returns `*runtime.Library`:

```go
func Library() *runtime.Library {
    return &runtime.Library{
        Name:        "cloud",
        Description: "Cloud resources.",
    }
}
```

The library can register resources, data sources, actions, functions, and optional configuration. The compiler reads the library's Go source for schemas, constraints, defaults, output fields, and function signatures. The compiled factory uses the registrations at runtime.

Common packages:

- `github.com/cloudboss/unobin/pkg/runtime` for library registration and typed resource contracts.
- `github.com/cloudboss/unobin/pkg/sdk/cfg` for library configuration schemas.
- `github.com/cloudboss/unobin/pkg/constraint` for Go-declared input constraints.
- `github.com/cloudboss/unobin/pkg/defaults` for Go-declared input defaults.
