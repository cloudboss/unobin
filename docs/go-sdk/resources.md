# Resources

A resource manages CRUD operations on an external object. It is implemented as
an instance of the `runtime.TypedResource[In, Out, Config any]` interface.

The following is a `runtime.TypedResource[File, *FileOutput, runtime.NoConfig]`:

```go
type File struct {
    Path    string
    Content string
}

type FileOutput struct {
    Path   string
    Size   int64
    Exists bool
}

func (f *File) SchemaVersion() int { return 1 }
func (f *File) ReplaceFields() []string { return []string{"path"} }

func (f *File) Create(ctx context.Context, cfg runtime.NoConfig) (*FileOutput, error) {
    return writeFile(f.Path, f.Content)
}

func (f *File) Read(
    ctx context.Context,
    cfg runtime.NoConfig,
    prior *FileOutput,
) (*FileOutput, error) {
    return readFile(prior)
}

func (f *File) Update(
    ctx context.Context,
    cfg runtime.NoConfig,
    prior runtime.Prior[File, *FileOutput],
) (*FileOutput, error) {
    return writeFile(f.Path, f.Content)
}

func (f *File) Delete(ctx context.Context, cfg runtime.NoConfig, prior *FileOutput) error {
    return removeFile(prior)
}
```

Register it:

```go
Resources: map[string]runtime.ResourceRegistration{
    "file": runtime.MakeResource[File, *FileOutput, runtime.NoConfig](),
}
```

Use `MakeResourceWith` when each receiver needs a constructed client, fake, or other external
state. The constructor runs each time the runtime needs a receiver, then the resource body is
decoded into that receiver.

## Read and not found

Return `runtime.ErrNotFound` from `Read` when the external object is absent. The runtime treats
that as a request to create it again.

`Read` runs during planning for resources that already have state. `Create`, `Update`, `Delete`,
and replacement work run only during apply.

## Update

`runtime.Prior[In, Out]` includes:

- `Inputs`, the prior evaluated inputs.
- `Outputs`, the prior resource outputs.
- `Observed`, the plan-time read result.

Use `runtime.Changed(prior.Inputs.Field, current.Field)` to compare decoded values.

## Replace fields

`ReplaceFields` names input fields that require replacement when changed. Other changes call `Update`.
