# Libraries

A Go library returns a `*runtime.Library`.

```go
func Library() *runtime.Library {
    return &runtime.Library{
        Name:        "files",
        Description: "File resources.",
        Resources: map[string]runtime.ResourceRegistration{
            "file": runtime.MakeResource[File, *FileOutput, runtime.NoConfig](),
        },
    }
}
```

The main fields are:

- `Name` and `Description` for human-readable metadata.
- `Configuration` for an optional library configuration schema.
- `Resources`, `DataSources`, and `Actions` for primitive node types.
- `Functions` for inline expression functions.
- `ResourceComposites`, `DataComposites`, and `ActionComposites` for generated UB libraries.

The compiler assigns the resolved library path. Library authors register the
types; factory source chooses the import alias.

`Configuration` can be declared inline or returned by another package's
`LibraryConfiguration()` function. Split packages let service packages share one
configuration schema while factories still import each service package by its
own path.

A factory imports the library and calls a registered type with `alias.type`:

```
imports: { files: 'github.com/example/files' }

resources: {
  config: files.file { path: input.path, content: input.content }
}
```
