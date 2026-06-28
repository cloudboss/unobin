# Configuration

A Go library can declare configuration that applies to every node under an import alias.

```go
type Configuration struct {
    Region      string
    Prefix      string
    MaxAttempts int64 `ub:"max-attempts"`
}

func (c Configuration) Defaults() []defaults.Default {
    return []defaults.Default{
        defaults.Value(c.Prefix, ""),
        defaults.Value(c.MaxAttempts, int64(3)),
    }
}

func (c Configuration) Constraints() []constraint.Constraint {
    return []constraint.Constraint{
        constraint.Must(constraint.NotEmpty(c.Region)).Message("region is required"),
        constraint.Must(constraint.AtLeast(c.MaxAttempts, 1)).
            Message("max-attempts must be positive"),
    }
}

func Library() *runtime.Library {
    return &runtime.Library{
        Name: "cloud",
        Configuration: &cfg.ConfigurationType[*Configuration]{
            Description: "Cloud connection settings.",
            New:         func() *Configuration { return &Configuration{} },
        },
    }
}
```

Use `runtime.NoConfig` as the config type parameter when a library has no configuration.

## Field model

Configuration structs use the same ordinary Go field model as resource, data source,
and action input structs:

- `string`, `bool`, `int64`, `float64`, and `any` map to UB scalar types.
- `[]T` maps to `list(T)`.
- `map[string]T` maps to `map(T)`.
- Nested structs map to UB objects.
- `*T` makes a field optional.
- `ub:"name"` changes the UB field name.
- `ub:"-"` omits a Go field from the UB schema.

Use `Defaults()` for non-pointer fields that may be omitted. Use `Constraints()`
for config validation. Defaults are applied before constraints and before the
decoded config reaches resources, data sources, actions, or functions.

## Source use

A factory input can use the configuration schema:

```ub
inputs: {
  cloud: {
    type: library-config('github.com/example/cloud')
    default: { region: 'us-west-2' }
  }
}

imports: { cloud: 'github.com/example/cloud' }

library-configs: {
  cloud: input.cloud
}
```

Every resource, data source, action, and function under the `cloud` alias receives
that config type at runtime.
