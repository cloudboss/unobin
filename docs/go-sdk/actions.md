# Actions

An action runs work and may return outputs. It has one `Run` method. It is implemented
as an instance of the `runtime.TypedAction[Out, Config any]` interface.

The following is a `runtime.TypedAction[*EchoOutput, runtime.NoConfig]`:

```go
type Echo struct {
    Text string
}

type EchoOutput struct {
    Text   string
    Length int64
}

func (e *Echo) Run(ctx context.Context, cfg runtime.NoConfig) (*EchoOutput, error) {
    return &EchoOutput{Text: e.Text, Length: int64(len(e.Text))}, nil
}
```

Register it:

```go
Actions: map[string]runtime.ActionRegistration{
    "echo": runtime.MakeAction[Echo, *EchoOutput, runtime.NoConfig](),
}
```

A factory uses it under `actions:`:

```
actions: {
  announce: util.echo {
    text: 'deploy complete'
  }
}
```

## Triggers

`@trigger` is an action-only meta key. Without it, an action reruns when its evaluated inputs change. Set `@trigger` to a value that should decide reruns:

```
actions: {
  read-back: std.exec-command {
    @trigger: resource.file.sha256
    argv:     ['cat', input.path]
  }
}
```

Use `@trigger: 'always'` for an action that should run every apply.

## Lock and timeout

`@lock` and `@timeout` can also apply to resources and data sources.

`@lock` serializes nodes that use the same lock name during apply:

```
@lock: 'kubectl'
```

`@timeout` sets a duration string for the resource, data source, or action:

```
@timeout: '30s'
```
