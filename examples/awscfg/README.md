# awscfg example

Shows a library whose configuration is `awscfg.Configuration`, the same AWS
connection schema unobin's s3 state backend and kms encrypter read. One
vocabulary then serves every place AWS settings appear: operator
configurations in the stack file, internal configurations in factory source,
and the `state:`/`encryption:` blocks. Because the type lives in unobin's own
packages, the compiler reads its fields when checking the factory, and
`schema show` lists them for operators.

The `cloud.describe` action makes no AWS calls. It reports the settings its
configuration selects (region, role ARN, and whether it would assume a role
or use ambient credentials), so the example runs anywhere.

Two things to notice in the sources:

- `dev.ub` declares the assume-role object once in `locals:` and references
  it from both configuration aliases. Config locals are static values, the
  file's own scope.
- `factory.ub` defines the `scoped` configuration internally, deriving its
  region from the `region` input, so an operator parameterizes it without
  owning it.

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

`./awscfg schema show` lists the configuration's fields under the `aws`
alias, with `scoped` marked internal and `default` and `east` owed from the
stack file.

## See the compile check

Misspell a field in the internal configuration in `factory.ub`, for example
`region:` to `regoin:`, and recompile:

```
Error: examples/awscfg/factory.ub:14:7: resolve: configurations.aws.scoped:
unknown field "regoin"
```

The same fields are enforced for `dev.ub` entries when a command loads the
stack file, at decode rather than compile.
