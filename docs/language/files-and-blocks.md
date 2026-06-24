# Files and blocks

Unobin projects have files for specific roles, with some reserved filenames.

## `factory.ub`

A factory file is called `factory.ub` and has one `factory:` declaration:

```
factory: {
  description: 'Deploys an app.'
  inputs: {}
  imports: {}
  library-configs: {}
  locals: {}
  constraints: []
  state-moves: []
  data-sources: {}
  resources: {}
  actions: {}
  outputs: {}
}
```

The commonly used blocks are `inputs`, `imports`, `locals`, `resources`, `data-sources`, `actions`, and `outputs`. A block can be omitted when it is empty.

## `project.ub`

A dependency project file is called `project.ub` and records direct requirements and replacements:

```
project: {
  requires: {
    'github.com/cloudboss/unobin-library-std': { version: 'v0.2.1' }
  }
  replace: {}
}
```

`unobin deps get` and `unobin deps sync` update this file.

## `project-lock.ub`

The `project-lock.ub` file records selected dependency versions and source facts. It is owned by `unobin deps sync`, and should not be edited by hand.

## Stack files

Stack files define encryption settings, state backend, factory inputs, and parallelism. The name of the stack file, minus its `.ub` extension, determines the stack name. The stack name is used in the state path in the storage backend, so changing the filename will operate on a different stack.

```
stack: {
  parallelism: 5

  factory: {
    inputs: { name: 'dev' }
  }

  state: local {
    path: '.unobin/state'
  }

  encryption: env-key {
    env-var: 'UB_STATE_KEY'
  }
}
```

A factory's `schema template` subcommand prints a starter stack file for a compiled factory.

## Library files

A UB library file declares composite types:

```
web: resource {
  inputs: { name: { type: string } }
  resources: {}
  outputs: {}
}
```

Composite categories are `resource`, `data-source`, and `action`.
