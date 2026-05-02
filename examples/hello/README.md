# hello

Hello-world stack. Writes a file via `local.file`, reads it back via
`core.command`, and exposes the captured output.

## Compile

`unobin compile` produces a stack binary. From the unobin repo root:

```
go run ./cmd/unobin compile \
  -p examples/hello/stack.ub \
  -o /tmp/hello-build \
  --replace-unobin="$(pwd)" \
  --unobin-version=v0.0.0 \
  --build
```

The `--replace-unobin` and `--unobin-version` flags plug the local
checkout into the generated `go.mod`; once unobin is published they
won't be needed. `--build` runs `go mod tidy` and `go build` against
the pinned Go toolchain at `~/.cache/unobin/bin/go-<version>`.

## Run

```
cd /tmp/hello-build
./hello plan -c "$OLDPWD"/examples/hello/dev.ub -o /tmp/hello-plan.json
./hello apply /tmp/hello-plan.json
./hello output
```

## Other subcommands

```
./hello version
./hello schema
./hello state list
./hello state show
```
