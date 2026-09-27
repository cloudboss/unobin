# Installation

Download a CLI archive for Linux or macOS from
[GitHub Releases](https://github.com/cloudboss/unobin/releases), together with its
`.sha256` file. Verify it with `sha256sum -c <archive>.sha256` on Linux or
`shasum -a 256 -c <archive>.sha256` on macOS, extract the archive, and place
`unobin` on your `PATH`.

Install the Unobin CLI with Go:

```
go install github.com/cloudboss/unobin/cmd/unobin@latest
```

Check that the command is on your path:

```
unobin version
```

The `unobin` CLI is used for developing factories. The end user of the factory only needs the compiled factory, not the `unobin` command.

A compiled factory is a Go program, and `unobin compile --build` runs `go mod tidy` and `go build` in the generated output directory. The dependency on Go is managed by `unobin`, so you do not need it preinstalled.

Editor packages are optional. See [Editor setup](../editors/index.md) if you want diagnostics, formatting, completion, and syntax highlighting for `.ub` files.
