# hello-nested-library

Hello-world stack that exercises nested UB libraries. The stack imports
only `greeter` (a local library), and `greeter.greeting` itself imports
the local `helloer` library. The deepest write goes through `std.fs-file`,
which `helloer` brings in behind its boundary. Each composite's own
`imports:` block resolves its libraries; the stack does not redeclare what
its composites use.

## Compile

```
go run ./cmd/unobin compile \
  -p examples/hello-nested-library/factory.ub \
  -o /tmp/hello-nested-library-build \
  --replace-unobin="$(pwd)" \
  --build
```

## Run

```
cd /tmp/hello-nested-library-build
./hello-nested-library plan --allow-version-mismatch \
  -c "$OLDPWD"/examples/hello-nested-library/dev.ub \
  -o /tmp/hello-nested-library-plan.json
./hello-nested-library apply /tmp/hello-nested-library-plan.json
./hello-nested-library output
./hello-nested-library state show
```

`state show` lists three entries:

* `resource.welcome` (the outer library-call record)
* `resource.welcome/resource.file` (the inner library-call record)
* `resource.welcome/resource.file/resource.this` (the deepest leaf)

`--allow-version-mismatch` is needed for the dev workflow because
`dev.ub` does not declare `factory.supported-versions`. In real
deployments the operator pins the binary's version+commit in the
stack file and the flag is unnecessary.
