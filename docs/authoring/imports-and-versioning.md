# Imports and versioning

Imports bind an alias to a library package path:

```
imports: {
  std: 'github.com/cloudboss/unobin-library-std'
  app: 'example.com/repo//ub/app'
  local: './local-lib'
}
```

The alias is used in node bodies. For Cloudboss libraries and their manuals, see [Cloudboss libraries](../libraries/index.md).

```
resources: {
  file: std.fs-file { path: input.path, content: input.content }
}
```

## Package path and project id

An import path can name any package directory inside a dependency project. The project id names the versioned project that owns that package.

For a package below a repository root project:

```
imports: {
  helloer: 'example.com/repo//ub/helloer'
}
```

The project requirement names the owning project:

```
project: {
  requires: {
    'example.com/repo': { version: 'v1.2.3' }
  }
}
```

## Repository subdirectory projects

A repository subdirectory can be its own project:

```
project: {
  requires: {
    'example.com/repo//library-c': { version: 'v1.2.3' }
  }
}
```

The repository root uses tags like `v1.2.3`. A subdirectory project uses tags prefixed with the project path, such as `library-c/v1.2.3` or `libs/core/v1.2.3`.

## Local relative imports

A relative import can target source governed by the same nearest `project.ub`.

```
imports: {
  notify: './notify'
}
```

Use this for local UB libraries in the same project.

## Go projects

A `.ub` file may import a Go package below a Go module. Generated `main.go` imports the package path, while generated `go.mod` requires and replaces the module path read from the selected project's `go.mod`.

Go modules follow Go's major-version path rule. UB project ids do not add `/vN` for major versions.
