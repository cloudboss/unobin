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

## Input constraints

Use `constraints:` to reject invalid combinations of factory inputs when a stack is planned. The compiler checks constraint syntax and verifies that `fields:` entries name declared inputs. The compiled factory evaluates the rules after stack inputs and defaults are combined, before it plans resources.

```
constraints: [
  { kind: exactly-one-of, fields: [input.image, input.build] },
  { kind: required-with, fields: [input.image, input.registry] },
  { kind: required-together, fields: [input.replicas[*].cert, input.replicas[*].key] },
  {
    kind:    predicate
    when:    input.tier == 'prod'
    require: @core.length(input.replicas) >= 2
    message: 'prod needs at least two replicas'
  },
  {
    kind:      predicate
    @for-each: input.replicas
    when:      @each.value.tls == true
    require:   @each.value.cert != null
    message:   'tls replicas need certs'
  },
]
```

Field-based constraints use a `fields:` list of input references. The set kinds are `exactly-one-of`, `at-least-one-of`, `at-most-one-of`, `required-together`, `required-with`, and `forbidden-with`. A field can point into an object or list item, such as `input.code.inline` or `input.listeners[0].cert`. A `[*]` field checks each list element; all splatted fields in one set rule must refer to the same list.

Predicate constraints evaluate `when:` first. If it is false, the rule passes. If it is true, `require:` must be true. A `message:` value replaces the default failure text.

A predicate can iterate over a list or map:

```
{
  kind:      predicate
  @for-each: input.replicas
  when:      @each.value.tls == true
  require:   @each.value.cert != null
}
```

If the iterable may be null, use an explicit fallback:

```
{
  kind:      predicate
  @for-each: input.replicas ?? []
  when:      @each.value.tls == true
  require:   @each.value.cert != null
}
```

Use `?? {}` for an optional map. A bare optional list or map is rejected because
`@each` needs a non-null iterable.

For nested iteration, use a list of binding objects:

```
{
  kind: predicate
  @for-each: [
    { @rule:       input.rules },
    { @transition: @rule.value.transitions },
  ]
  when:    true
  require: @transition.value.days != null
}
```

Factory constraints may read `input.*` values only. They may call `@core` functions and imported Go functions. Use them for the factory's input contract; Go library constraints still check each resource, data source, or action body wherever that library kind is called.

## Resources, data sources, and actions

Resource, data source, and action bodies call imported library kinds:

```
actions: {
  read-back: std.exec-command {
    @trigger: resource.hello.sha256
    argv:     ['cat', input.path]
  }
}

data-sources: {
  github: aws.iam-openid-connect-provider {
    url: input.oidc-provider-url
  }
}

resources: {
  app: std.fs-file {
    path: local.path
    content: input.content
  }
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

The `library-config(...)` path may name a separate configuration package. This
is useful when one repository has service packages that share a config package:

```
inputs: {
  aws: { type: library-config('example.com/aws//config') }
}

imports: {
  s3: 'example.com/aws//s3'
}

library-configs: {
  s3: input.aws
}
```

The imported service package can set `Library().Configuration` to
`config.LibraryConfiguration()`. The config package must provide
`LibraryConfiguration()`.

To configure multiple library instances with different configurations, import the
library again under a different alias, and bind the configurations separately:

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
