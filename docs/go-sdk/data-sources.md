# Data sources

A data source reads external data and exposes outputs to expressions. It does not manage the external object.

```go
type ReadFile struct {
    Path string
}

type ReadFileOutput struct {
    Path    string
    Content string
    Size    int64
}

func (r *ReadFile) Read(
    ctx context.Context,
    cfg runtime.NoConfig,
) (*ReadFileOutput, error) {
    return readFile(r.Path)
}
```

Register it:

```go
DataSources: map[string]runtime.DataSourceRegistration{
    "read-file": runtime.MakeDataSource[ReadFile, *ReadFileOutput, runtime.NoConfig](),
}
```

A factory uses it under `data-sources:`:

```
data-sources: {
  source: files.read-file { path: input.path }
}

outputs: {
  content: { value: data-source.source.content }
}
```

Data sources can run during planning when their inputs are known. Their outputs can feed resources, actions, locals, constraints, and outputs.
