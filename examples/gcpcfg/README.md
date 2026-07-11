# gcpcfg example

Shows a library whose config is `gcpcfg.Configuration`, the Google Cloud
connection schema used by Unobin's `gcs` state backend and `gcp-kms` encrypter.
The factory exposes the config as an ordinary input with
`library-config('<path>')`, then assigns that input to import aliases with
`library-configs:`.

The `cloud.describe` action makes no GCP calls. It reports the settings its
alias config selects: project, region, service endpoints, and the first OAuth
scope.

Things to notice in the sources:

- `dev.ub` declares the Google Cloud config once in `locals:` and reuses it for
  factory inputs, `state: gcs`, and `encryption: gcp-kms`.
- `factory.ub` has two aliases for the same Go library. `gcp-scoped` derives
  its region from the ordinary `region` input with `@core.merge`.
- `state: gcs.kms-key-name` is the GCS CMEK setting for stored objects.
  `encryption: gcp-kms.key-id` is the envelope encryption key.

## Try it

This checks the stack file and builds the factory without contacting GCP:

```
go run ./cmd/unobin compile \
  -p examples/gcpcfg/factory.ub \
  -o /tmp/gcpcfg-build \
  --replace-unobin="$(pwd)" \
  --build

cd /tmp/gcpcfg-build
./gcpcfg validate --allow-version-mismatch \
  -c "${OLDPWD}/examples/gcpcfg/dev.ub"
./gcpcfg schema show
```

Expected validation output:

```
OK
```

`./gcpcfg schema show` lists `gcp-config` under inputs and expands the fields stack
authors may provide.
