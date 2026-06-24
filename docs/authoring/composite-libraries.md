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

## State moves

A composite can declare state moves relative to each call site:

```
state-moves: [
  { from: resource.old, to: resource.this },
]
```
