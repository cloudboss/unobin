# hello-nested-library

Hello-world stack that exercises nested UB libraries. The stack imports
only `greeter` (a local library), and `greeter.greeting` itself imports
the remote `github.com/cloudboss/unobin-libraries-scratch//ub/helloer`
package. The manifest pins the owning
`github.com/cloudboss/unobin-libraries-scratch` project, and the lock records
that project rather than the package path.

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
./hello-nested-library state list
./hello-nested-library state show greeter.greeting@resource.welcome
```

`state list` lists three entries:

* `greeter.greeting@resource.welcome` (the outer library-call record)
* `helloer.hello@resource.welcome/resource.file` (the inner library-call record)
* `std.fs-file@resource.welcome/resource.file/resource.this` (the deepest leaf)

`--allow-version-mismatch` is needed for the dev workflow because
`dev.ub` does not declare `factory.supported-versions`. In real
deployments the operator pins the binary's version+commit in the
stack file and the flag is unnecessary.
