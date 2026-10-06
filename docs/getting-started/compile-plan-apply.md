# Compile, plan, apply

Compile writes a generated Go program. With `--build`, it also builds the factory executable.

The compiler records generated source and captured assets in `.unobin-generated.json`. Recompilation validates that ownership, removes obsolete generated UB packages and assets, and preserves unrelated files. Edits to owned source or collisions with authored files stop compilation before publication.

The compiler replaces `go.mod` and resets `go.sum` for the current imports. A build then runs `go mod tidy`, verifies the selected dependencies, and computes factory identity before publishing the executable. Generation without `--build` reports no verified revision. Module files and built executables have separate update rules from source; keep authored code in the source project.

For output from older compilers without a manifest, choose a fresh `-o` directory.

From the factory source directory:

```
unobin compile \
  -o ./build \
  --build \
  --library-path github.com/example/appdeploy
```

`--profile full` is the default. `--profile local` includes local state and environment-key encryption without the built-in cloud state and encryption SDKs:

| Profile | State backends | Encryption types |
| --- | --- | --- |
| `full` | `local`, `s3`, `gcs` | `env-key`, `kms`, `gcp-kms`, `noop` |
| `local` | `local` | `env-key`, `noop` |

The profile selects built-in state and encryption implementations. Imported provider libraries retain their own dependencies. Stack files select among the implementations included in the binary; unavailable selections fail before state access. Use `schema state` to inspect the available types and configuration fields:

```
unobin compile --profile local -o ./build --build
./build/appdeploy schema state
./build/appdeploy schema state --format json
```

The linked factory revision includes the generated entry point and its selected dependencies, so changing profiles changes the verified revision.

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
