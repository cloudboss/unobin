# Composite libraries

A composite library is UB source that exports reusable types.

A resource composite looks like this:

```
greeting: resource {
  description: 'Writes a greeting file.'

  inputs: {
    message: { type: string }
    path:    { type: string }
  }

  imports: { std: 'github.com/cloudboss/unobin-library-std' }

  resources: {
    this: std.fs-file {
      path:    input.path
      content: input.message
    }
  }

  outputs: {
    path:   { value: resource.this.path }
    size:   { value: resource.this.size }
    sha256: { value: resource.this.sha256 }
  }
}
```

Call it from a factory through an import alias:

```
imports: { greeter: './greeter' }

resources: {
  hello: greeter.greeting {
    message: input.message
    path:    input.path
  }
}
```

## Categories

Composite categories are separate namespaces:

- `name: resource { ... }`
- `name: data-source { ... }`
- `name: action { ... }`

Resource composites contain resources and expose declared outputs. Data source composites expose data-source outputs. Action composites contain actions.

## Interface

A composite is opaque to callers except for its declared outputs. Internal resources, data sources, actions, and locals are implementation details of the composite body.

## Scope and configuration

Each composite kind has a lexical scope. Its inputs, imports, library configs, locals, assets, and node names belong to its body. A call evaluates its arguments in the caller's environment and evaluates the composite body in a separate environment. Pass caller values through declared inputs to make them available inside the body.

Every invocation has its own environment, including each `@for-each` instance. Nested composites resolve names through their own imports and declarations. A resource, data source, and action can use the same name because their categories are separate namespaces.

A Go-library configuration belongs to an import alias in the containing factory or composite instance. Resources, data sources, and actions under that alias use its configuration binding. Two aliases can have different configurations for the same library. The same alias in a different composite instance has a separate binding.

A UB library groups exported kinds. Each kind accepts configuration through its declared inputs. In this example, `example.com/cloud` is a placeholder Go library:

```ub
service: resource {
  inputs: {
    cloud: { type: library-config('example.com/cloud') }
    name: { type: string }
  }
  imports: { cloud: 'example.com/cloud' }
  library-configs: { cloud: input.cloud }
  resources: { this: cloud.instance { name: input.name } }
  outputs: { id: { value: resource.this.id } }
}
```

The factory passes the configurations explicitly:

```ub
inputs: {
  east: { type: library-config('example.com/cloud') }
  west: { type: library-config('example.com/cloud') }
}
imports: { services: './services' }
resources: {
  east: services.service { cloud: input.east, name: 'east' }
  west: services.service { cloud: input.west, name: 'west' }
}
```

Plan and apply evaluate configuration in the enclosing instance. Refresh and destroy reconstruct that instance's environment from current factory inputs and saved node values. Arguments that cannot be evaluated for a removed call or iteration key use their saved values. Keep the owning library import available until the removed composite's resources have been destroyed.

An `@for-each` composite can contain nested composites. A second `@for-each` inside that repeated body is rejected at compile time.

## State moves

A composite can declare state moves relative to each call site:

```
state-moves: [
  { from: resource.old, to: resource.this },
]
```
