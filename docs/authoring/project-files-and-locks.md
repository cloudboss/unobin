# Project files and locks

A dependency project is a versioned directory marked by `project.ub` or `go.mod`.

UB projects use `project.ub`:

```
project: {
  requires: {
    'github.com/cloudboss/unobin-library-std': { version: 'v0.2.1' }
  }
  replace: {}
}
```

Go library projects use `go.mod`. `unobin deps sync` manages UB projects only. Use Go commands for Go modules.

## `project.ub`

`project.ub` records direct dependency requirements and local replacements. The `requires` block names dependency project ids, not every import package below them.

## `project-lock.ub`

`project-lock.ub` records the selected versions, commits, hashes, and toolchain facts used by compile. `unobin deps sync` owns the lock.

Compile reads the lock. When source imports, requirements, or replacements have changed, run:

```
unobin deps sync
```

## Nested project boundaries

A nested `project.ub` or `go.mod` starts a new project boundary. Syncing an ancestor project does not scan below that nested project. Run sync from the nested project or pass a path:

```
unobin deps sync -p library-c
```
