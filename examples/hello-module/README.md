# hello-module

Hello-world stack that calls a composite from a local UB module. The
`greeter` module exports a `greeting` type that wraps `local.file`,
and the stack instantiates it as `greeter.greeting.welcome`.

## Compile

```
go run ./cmd/unobin compile \
  -p examples/hello-module/stack.ub \
  -o /tmp/hello-module-build \
  --replace-unobin="$(pwd)" \
  --unobin-version=v0.0.0 \
  --build
```

## Run

```
cd /tmp/hello-module-build
./hello-module plan -c "$OLDPWD"/examples/hello-module/dev.ub -o /tmp/hello-module-plan.json
./hello-module apply /tmp/hello-module-plan.json
./hello-module output
./hello-module state list
```

`state list` shows two entries: the module-call record at
`resource.greeter.greeting.welcome` and the internal leaf at
`resource.greeter.greeting.welcome/local.file.this`.
