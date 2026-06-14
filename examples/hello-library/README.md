# hello-library

Hello-world stack that calls a composite from a local UB library. The
`greeter` library exports a `greeting` type that wraps `std.fs-file`,
and the stack instantiates it as `greeter.greeting.welcome`.

## Compile

```
go run ./cmd/unobin compile \
  -p examples/hello-library/factory.ub \
  -o /tmp/hello-library-build \
  --replace-unobin="$(pwd)" \
  --build
```

## Run

```
cd /tmp/hello-library-build
./hello-library plan --allow-version-mismatch \
  -c "$OLDPWD"/examples/hello-library/dev.ub -o /tmp/hello-library-plan.json
./hello-library apply /tmp/hello-library-plan.json
./hello-library output
./hello-library state list
```

`--allow-version-mismatch` is needed for the dev workflow because
`dev.ub` does not declare `factory.supported-versions`. In real
deployments the operator pins the binary's version+commit in the
stack file and the flag is unnecessary.

`state list` shows two entries: the library-call record at
`resource.greeter.greeting.welcome` and the internal leaf at
`resource.greeter.greeting.welcome/std.fs-file.this`.
