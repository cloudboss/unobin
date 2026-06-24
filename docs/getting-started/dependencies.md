# Dependencies

Unobin uses Go's Minimal Version Selection model for UB dependencies. Each requirement is a minimum acceptable version. If one imported library requires a project at `v1.2.0` and another requires the same project at `v1.5.0`, Unobin selects `v1.5.0`.

`project.ub` records those minimum versions for the projects your source imports. `unobin deps sync` resolves the complete dependency graph, writes the selected versions to `project-lock.ub`, and compile uses the lock. Unobin does not select a newer release just because it exists; the selected version changes when a requirement changes or a dependency is updated.

Imports are defined in source:

```
imports: {
  std: 'github.com/cloudboss/unobin-library-std'
  notify: './notify'
}
```

Remote imports need a project requirement in `project.ub`. The project file records minimum dependency versions. For Cloudboss libraries and their manuals, see [Cloudboss libraries](../libraries/index.md).

```
project: {
  requires: {
    'github.com/cloudboss/unobin-library-std': { version: 'v0.2.1' }
  }
}
```

`project-lock.ub` records selected versions, commits, hashes, and toolchain facts. Compile reads the lock and reports stale metadata.

A directory containing a `factory.ub` cannot be imported except from within the directory itself. The convention is to import `.` as `self`, though the alias can be any name:

```
imports: {
  self: '.'
}

resources: {
  shared: self.cluster { ... }
}
```

## Commands

To add or update a direct dependency:

```
unobin deps get github.com/cloudboss/unobin-library-std@v0.2.1
```

To reconcile imports with `project.ub` and `project-lock.ub`:

```
unobin deps sync
```

To inspect the lock:

```
unobin deps list
```

To verify cached dependency sources against the lock:

```
unobin deps verify
```

## Local replacements

For local development, replace an exact project id with a local path:

```
project: {
  requires: {
    'example.com/repo//library-c': { version: 'v1.2.3' }
  }
  replace: {
    'example.com/repo//library-c': './library-c'
  }
}
```

Run `unobin deps sync` after changing imports, requirements, or replacements.
