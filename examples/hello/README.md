# hello

Hello-world stack. Writes a file via `std.fs-file`, reads it back via
`std.exec-command`, and exposes the captured output.

## Compile

`unobin compile` produces a stack binary. From the unobin repo root:

```
go run ./cmd/unobin compile \
  -p examples/hello/factory.ub \
  -o /tmp/hello-build \
  --replace-unobin="$(pwd)" \
  --build
```

The `--replace-unobin` flag plugs the local checkout into the
generated `go.mod`; once unobin is published it won't be needed.
`--build` runs `go mod tidy` and `go build` against the pinned Go
toolchain at `~/.cache/unobin/bin/go-<version>`.

## Run

```
cd /tmp/hello-build
./hello plan --allow-version-mismatch \
  -c "$OLDPWD"/examples/hello/dev.ub -o /tmp/hello-plan.json
./hello apply /tmp/hello-plan.json
./hello output
```

`--allow-version-mismatch` is needed for the dev workflow because
`dev.ub` does not declare `factory.supported-versions`. In real
deployments the operator pins the binary's version+commit in the
stack file and the flag is unnecessary.

Set `UB_STATE_KEY` to a base64-encoded 32-byte key to encrypt
state snapshots and plan files at rest:

```
export UB_STATE_KEY=$(head -c 32 /dev/urandom | base64)
```

## Other subcommands

```
./hello version
./hello schema
./hello state list
./hello state show
```
