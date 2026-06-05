# locals

Shows how a `locals:` block computes values once and reuses them. The
stack builds a cluster name from its inputs, layers more locals on top,
and feeds them into a resource, an action, and the outputs.

What each local demonstrates:

- **interpolation over inputs** — `cluster` joins `env` and `region`
  into one name.
- **a local that reads another local** — `banner` builds on `cluster`,
  so changing an input flows through both.
- **a boolean expression** — `is-prod` is just `var.env == 'prod'`.
- **one value used in two places** — `summary-path` is the file's path
  and the `cat` argument, written once.
- **reading a resource output** — `file-hash` is the written file's
  sha256. It is not computed up front; it resolves only after the file
  exists, and the `read-back` action uses it as its `@trigger`.

Because the action reads `std.fs-file-hash` and `local.summary-path`, it
depends on the file through the locals even though it never names the
resource directly, so it runs after the file is written.

## Compile

From the unobin repo root:

```
go run ./cmd/unobin compile \
  -p examples/locals/main.ub \
  -o /tmp/locals-build \
  --replace-unobin="$(pwd)" \
  --build
```

## Run

```
cd /tmp/locals-build
export UB_STATE_KEY=$(head -c 32 /dev/urandom | base64)
./locals plan --allow-version-mismatch \
  -c "$OLDPWD"/examples/locals/dev.ub -o /tmp/locals-plan.json
./locals apply /tmp/locals-plan.json
./locals output -c "$OLDPWD"/examples/locals/dev.ub
```

A second `plan` after `apply` reports no changes: `file-hash` now
resolves to the stored sha256, so the `read-back` trigger is unchanged
and the action is skipped.
