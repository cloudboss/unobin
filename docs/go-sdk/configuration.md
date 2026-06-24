# Configuration

A Go library can declare configuration that applies to every node under an import alias.

```go
type Configuration struct {
    Region cfg.String
    Prefix *cfg.String
}

func Library() *runtime.Library {
    return &runtime.Library{
        Name: "cloud",
        Configuration: &cfg.ConfigurationType[*Configuration]{
            Description: "Cloud connection settings.",
            New: func() *Configuration {
                return &Configuration{
                    Prefix: &cfg.String{Default: ""},
                }
            },
        },
    }
}
```

Use `runtime.NoConfig` as the config type parameter when a library has no configuration.

## Wrapper types

Configuration structs use wrapper types from `pkg/sdk/cfg`:

- `cfg.String`
- `cfg.Integer`
- `cfg.Number`
- `cfg.Boolean`
- `cfg.Null`
- `cfg.Any`
- `cfg.List[T]`
- `cfg.Map[T]`
- `cfg.Object[T]`

A pointer field makes a nested value optional. Wrapper fields can set descriptions, defaults, and validators.

## Source use

A factory input can use the configuration schema:

```
inputs: {
  cloud: {
    type: library-config('github.com/example/cloud')
    default: {}
  }
}

imports: { cloud: 'github.com/example/cloud' }

library-configs: {
  cloud: input.cloud
}
```

Every resource, data source, action, and function under the `cloud` alias receives that config type at runtime.
