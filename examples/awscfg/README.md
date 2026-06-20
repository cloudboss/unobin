# awscfg example

Shows a library whose config is `awscfg.Configuration`, the same AWS connection
schema unobin's s3 state backend and kms encrypter read. The factory exposes the
config structs as ordinary inputs with `library-config('<path>')`, then assigns
those inputs to import aliases with `library-configs:`.

The `cloud.describe` action makes no AWS calls. It reports the settings its
alias config selects: region, role ARN, and whether it would assume a role or
use ambient credentials.

Two things to notice in the sources:

- `dev.ub` declares the assume-role object once in `locals:` and references it
  from both config inputs.
- `factory.ub` has three aliases for the same Go library. `aws-scoped` derives
  its region from the ordinary `region` input with `@core.merge`.

## Try it

```
go run ./cmd/unobin compile \
  -p examples/awscfg/factory.ub \
  -o /tmp/awscfg-build \
  --replace-unobin="$(pwd)" \
  --build

cd /tmp/awscfg-build
./awscfg plan --allow-version-mismatch \
  -c "${OLDPWD}/examples/awscfg/dev.ub" \
  -o /tmp/awscfg-plan.json
./awscfg apply /tmp/awscfg-plan.json
./awscfg output -c "${OLDPWD}/examples/awscfg/dev.ub"
```

Expected output:

```
default-role: 'arn:aws:iam::123456789012:role/unobin-example'
east-region: 'us-east-2'
scoped-region: 'us-west-2'
```

`./awscfg schema` lists `aws-config` and `east-config` under inputs and expands
the fields stack authors may provide.

## See the compile check

Misspell a field in a library config binding in `factory.ub`, for example
`region:` to `regoin:`, and recompile:

```
Error: examples/awscfg/factory.ub:27:41: type: object has no field "regoin"
```

The same fields are checked for `dev.ub` input values when a command reads the
stack file.
