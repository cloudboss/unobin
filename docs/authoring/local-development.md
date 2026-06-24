# Local development

Use replacements while developing a factory and its libraries together.

## Replace a dependency project

In `project.ub`, replace the exact project id with a local path:

```
project: {
  requires: {
    'example.com/repo//library-c': { version: 'v1.2.3' }
  }
  replace: {
    'example.com/repo//library-c': '../repo/library-c'
  }
}
```

Run sync after changing replacements:

```
unobin deps sync
```

The replacement key is a project id. For a nested project, replace the nested project id exactly. A parent replacement does not satisfy a nested project's replacement.

## Replace Unobin itself

When compiling examples against a local checkout, use `--replace-unobin`:

```
unobin compile \
  -p examples/hello/factory.ub \
  -o /tmp/hello-build \
  --replace-unobin "$(pwd)" \
  --build
```

This substitutes the local checkout for `github.com/cloudboss/unobin` in the generated Go module.

## Replace Go modules

For an imported Go library module, use `--replace-go-module`:

```
unobin compile \
  --replace-go-module example.com/cloud=../cloud \
  -o ./build \
  --build
```

The flag takes `module-path=local-path` and can be repeated.

## Go libraries

If the nearest project marker is `go.mod`, use Go commands for that module. `unobin deps sync` manages UB projects.
