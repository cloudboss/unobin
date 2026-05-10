# hello-nested-module

Hello-world stack that exercises nested UB modules. The stack imports
only `greeter` (a local module), and `greeter.greeting` itself imports
the remote `helloer` module from `cloudboss/unobin-modules-scratch`.
The deepest write goes through `local.file`, which `helloer` brings in
behind its boundary. Each composite's own `imports:` block resolves
its modules; the stack does not redeclare what its composites use.

## Compile

```
go run ./cmd/unobin compile \
  -p examples/hello-nested-module/stack.ub \
  -o /tmp/hello-nested-module-build \
  --replace-unobin="$(pwd)" \
  --unobin-version=v0.0.0 \
  --build
```

## Run

```
cd /tmp/hello-nested-module-build
./hello-nested-module plan --allow-version-mismatch \
  -c "$OLDPWD"/examples/hello-nested-module/dev.ub \
  -o /tmp/hello-nested-module-plan.json
./hello-nested-module apply /tmp/hello-nested-module-plan.json
./hello-nested-module output
./hello-nested-module state show
```

`state show` lists three entries:

* `resource.greeter.greeting.welcome` (the outer module-call record)
* `resource.greeter.greeting.welcome/helloer.hello.file` (the inner
  module-call record)
* `resource.greeter.greeting.welcome/helloer.hello.file/local.file.this`
  (the deepest leaf)

`--allow-version-mismatch` is needed for the dev workflow because
`dev.ub` does not declare `stack.supported-versions`. In real
deployments the operator pins the binary's version+commit in
`config.ub` and the flag is unnecessary.
