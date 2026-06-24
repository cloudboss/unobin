# Compile, plan, apply

Compile writes a generated Go program. With `--build`, it also builds the factory executable.

From the factory source directory:

```
unobin compile \
  -o ./build \
  --build \
  --library-path github.com/example/appdeploy
```

The output directory contains an executable named after the factory directory:

```
./build/appdeploy version
```

Generate a starter stack file from the factory input schema:

```
./build/appdeploy schema template -o dev.ub
```

Edit `dev.ub` and fill in the inputs:

```
stack: {
  factory: {
    inputs: {
      message: 'Hello from unobin'
      path:    '/tmp/unobin-greeting.txt'
    }
  }

  state: local {
    path: '.unobin/state'
  }

  encryption: noop {}
}
```

Plan writes an encrypted plan file. Apply consumes that plan file:

```
./build/appdeploy plan -c dev.ub -o plan.json
./build/appdeploy apply plan.json
```

Use `--ui` to watch apply in a browser:

```
./build/appdeploy apply --ui plan.json
```

Apply consumes a plan file so the command runs exactly what was reviewed. If source, inputs, state backend, or encryption settings change, compute a new plan.
