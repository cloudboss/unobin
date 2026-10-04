# Libraries

A Go library returns a `*runtime.Library`.

```go
func Library() *runtime.Library {
    return &runtime.Library{
        Name:        "files",
        Description: "File resources.",
        Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
        Resources: map[string]runtime.ResourceRegistration{
            "file": runtime.MakeResource[File, *FileOutput, runtime.NoConfig](
                fileDefinition(),
            ),
        },
    }
}
```

The main fields are:

- `Name` and `Description` for human-readable metadata.
- `Compatibility` for the implementation API required by the package.
- `Configuration` for an optional library configuration schema.
- `Resources`, `DataSources`, and `Actions` for primitive node types.
- `Functions` for inline expression functions.
- `ResourceComposites`, `DataComposites`, and `ActionComposites` for generated UB libraries.

The compiler assigns the resolved library path. Library authors register the
types; factory source chooses the import alias.

Declare `Compatibility` directly in the returned record with literal strings.
`RequiredAPI` is a positive major and nonnegative minor such as `1.0`, without
leading zeroes, a `v` prefix, or a patch component. The package declares the
lowest minor it needs. A tool implementing that major accepts requirements up
to its advertised minor. Library API numbers are independent of library and
Unobin release numbers; API `1.x` preserves the earlier `1.x` contracts.

An optional `SuggestedUnobinVersion` names a full release such as `v0.12.0`
that the publisher has validated. It may include a prerelease suffix, and must
omit build metadata. It is an upgrade hint and does not determine compatibility.
Generators emit a literal API from the tool's descriptor and leave that hint
empty. Helper calls, constants, and computed strings cannot replace the literal
compatibility declaration.

`Configuration` can be declared inline or returned by another package's
`LibraryConfiguration()` function. Split packages let service packages share one
configuration schema while factories still import each service package by its
own path.

Configuration entry points also export a compatible `Library()` record and
register the same configuration as `LibraryConfiguration()`. See
[Configuration](configuration.md#separate-configuration-packages).

A factory imports the library and calls a registered type with `alias.type`:

```
imports: { files: 'github.com/example/files' }

resources: {
  config: files.file { path: input.path, content: input.content }
}
```
