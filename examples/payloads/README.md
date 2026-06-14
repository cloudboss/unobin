# payloads

Shows the two types for data the stack does not fully describe: an
opaque value that passes through whole, and an open object that
declares only the fields the stack reads.

What each piece demonstrates:

- **opaque passes through unread** — `policy` is `type: opaque`: the
  stack holds it, outputs it, and serializes it, but reading inside it
  is a compile error. `@core.to-json` is the text exit; the document
  reaches `policy.json` whole, never inspected.
- **open admits, declaring reads** — `event` is
  `open(object({ kind: string  id: string }))`. The stack dots into
  the declared fields; whatever else the operator supplies rides along
  and survives into the serialized file. Open changes what may be
  present, never what may be read: dotting an undeclared field is the
  same compile error a closed object gives.
- **closed stays the default** — a plain `object({ ... })` still
  rejects undeclared fields at decode, which is the typo catch.

Try it: add `var.policy.version` to an output and the compile error
shows the open(object) form that declares the field; add another key
under `event` in `dev.ub` and it flows into the event file, while
misspelling `kind` fails decode.

## Compile

From the unobin repo root:

```
go run ./cmd/unobin compile \
  -p examples/payloads/factory.ub \
  -o /tmp/payloads-build \
  --replace-unobin="$(pwd)" \
  --build
```

## Run

```
cd /tmp/payloads-build
export UB_STATE_KEY=$(head -c 32 /dev/urandom | base64)
./payloads plan --allow-version-mismatch \
  -c "$OLDPWD"/examples/payloads/dev.ub -o /tmp/payloads-plan.json
./payloads apply /tmp/payloads-plan.json
./payloads output -c "$OLDPWD"/examples/payloads/dev.ub
```

`event-deploy-rollout-7.json` holds the whole envelope, the
undeclared `region` and `attempt` included; `policy.json` holds the
policy document the stack never read.
