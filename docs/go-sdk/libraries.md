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

## Maintaining compatibility

API `1.0` is the baseline for typed resource definitions and lifecycle methods.
Ordinary fixes retain the API identifier. A library that uses a new contract
addition declares the minor that introduced it. Later Unobin implementations
of that major preserve the earlier contracts, so a compatible tool can use the
same tagged library without an edited declaration.

The compiler checks selected library packages and packages reached through
configuration forwarding. Nested Go modules have their own selected source.
Unused packages do not impose API requirements on an imported subset.
Configuration-only packages receive the same declaration checks; their core Go
module floor applies when the generated factory links them. A local replacement
uses its current source, including edits made during an editor session.

For an older library without metadata, migrate its resource definitions and
lifecycle methods to the typed contract, then add a literal `RequiredAPI: "1.0"`
to each exported library record, including configuration entry points. Run
[compiled consumer tests](testing.md#consumer-fixtures) before publishing a new
tag. Keep historical tags unchanged. New CLIs cannot infer an API from those
libraries' release numbers, and old CLIs retain their existing selection policy.

Keep the required Unobin Go module version and the module's minimum Go version
accurate as separate requirements. Dependency-local Go `replace` and `toolchain`
directives do not select the factory's runtime. If you set
`SuggestedUnobinVersion`, validate that release in CI. It records a tested
combination and remains an upgrade hint for users.

## Regenerating provider code

`unobin generate golibrary` records its outputs in `.unobin-generated.json`.
It owns the resource and data declarations (`*_rsrc.go` and `*_dsrc.go`),
`library.go`, and generated configuration. These files include a generated-code
marker. Regeneration rejects edits to them and collisions with files it does
not own before changing any output.

Implement lifecycle methods in the corresponding `*_impl.go` files. The
generator creates these files once and preserves their contents on subsequent
runs. It also creates `go.mod` once; use Go tooling to manage dependencies.
Other files with distinct paths remain untouched.

When a schema removes a type, regeneration removes its declarations and an
untouched lifecycle stub. If its lifecycle file contains edits, regeneration
stops so you can review that implementation before removing it.

For output generated before ownership manifests were introduced, generate into
a fresh directory with `-o`, then transfer your lifecycle implementations and
other authored code. Existing directories without a manifest are rejected.
