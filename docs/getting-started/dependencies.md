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

Remote imports need a project requirement in `project.ub`. The project file
records minimum dependency versions. For Cloudboss libraries and their manuals,
see [Cloudboss libraries](../libraries/index.md).

Factory inputs that use `library-config('...')` also count as direct
dependencies. This includes config packages that are not present in `imports`.

```
project: {
  requires: {
    'github.com/cloudboss/unobin-library-std': { version: 'v0.2.1' }
  }
}
```

`project-lock.ub` records selected versions, commits, hashes, and toolchain facts. Compile reads the lock and reports stale metadata.

Compile reads remote Go and UB package source at the locked commit, using the
commit cache when available. An unavailable commit is an error; compilation
does not substitute a current tag. The selected release version still determines
the generated Go module requirement and its required module-path major suffix,
including for prereleases.

Compile, source check, graph output, and dependency get/sync check the running
CLI against the project's `unobin-version` pin before reading library schemas
or selecting dependency versions. A development CLI needs a local Unobin core
replacement with the same library API descriptor as that CLI. A matching core
replacement permits a different project release pin; compile reports which
replacement runs. A missing or different descriptor fails before schema-source
fallback. Explicit core replacement options take precedence over the project's
core replacement, while more specific library replacements still apply.

A Go library can forward its configuration registration to another selected
module or an effective local replacement without a separate UB schema import.
Unobin checks the defining configuration package and records its source in the
lock and preflight manifest. The module root does not need a library record,
and unused packages are not checked. Configuration forwarding from a linked
library also adds that module to the generated Go requirements.

With `compile --build`, Unobin checks Go's selected modules after `go mod tidy`
and before building. Each linked library and configuration package must use the
inspected version or local replacement directory. A higher selected library
version requires updating its project floor and running `unobin deps sync`.
Different compatibility metadata at the same tag requires a new immutable
library tag. Modules used only for schema inspection do not need to appear in
the Go build. These checks can fail after generated files or `go.sum` are written;
compile reports those file changes with the error.

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

Get and sync check the selected library contracts and package sources before
writing dependency files. Get reports a version after both writes succeed.
Validation failures leave existing files unchanged and absent files absent.
A filesystem error during writing can leave `project.ub` updated before the
lock is written; the command reports those completed file changes.

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
