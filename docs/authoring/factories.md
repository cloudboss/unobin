# Factories

A factory source describes the stack family you want to compile into one executable.

A small factory has inputs, imports, resources, and outputs:

```
factory: {
  description: 'Writes a file.'

  inputs: {
    path:    { type: string }
    content: { type: string }
  }

  imports: { std: 'github.com/cloudboss/unobin-library-std' }

  resources: {
    file: std.fs-file {
      path:    input.path
      content: input.content
    }
  }

  outputs: {
    path: { value: resource.file.path }
  }
}
```

## Inputs and locals

Inputs are values passed to the factory from the stack file. Locals are file-scoped expressions computed from inputs, resources, data sources, actions, and other locals where dependencies permit it.

```
locals: {
  path: $'{{ input.base-path }}/app.conf'
}
```

## Resources, data sources, and actions

Resource, data source, and action bodies call imported library types:

```
resources: {
  app: std.fs-file { path: local.path, content: input.content }
}
```

## State moves

State moves rename entries in state without recreating the external object:

```
state-moves: [
  { from: resource.old, to: resource.app },
]
```

## Library configs

If an imported Go library declares a configuration schema, bind the import alias to the configuration in `library-configs`:

```
inputs: {
  cloud-config: {
    @sensitive: true
    type:       library-config('github.com/example/cloud')
    default:    {}
  }
}

imports: { cloud: 'github.com/example/cloud' }

library-configs: {
  cloud: input.cloud-config
}
```

Inputs may use `library-config('...')` to define the configuration type, passing in the library path. Thus, ordinary inputs are used to configure libraries; there is no separate configuration mechanism like Terraform uses for providers.

To configure multiple library instances with different configurations, import the library again under a different alias, and bind the configurations separately:

```
inputs: {
  cloud-config: {
    @sensitive: true
    type:       library-config('github.com/example/cloud')
    default:    {}
  }
}

locals: {
  cloud-config-east: @core.merge(input.cloud-config, { region: 'east' })
}

imports: {
  cloud:      'github.com/example/cloud'
  cloud-east: 'github.com/example/cloud'
}

library-configs: {
  cloud:      input.cloud-config
  cloud-east: local.cloud-config-east
}
```

## Compiling

Compile from the source root:

```
unobin compile -o ./build --build --library-path github.com/example/appdeploy
```

Alternatively, pass the path to the source root:

```
unobin compile -o ./build --build --library-path github.com/example/appdeploy -p ./factory-abc
```

The resulting executable is the factory.
