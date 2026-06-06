# optionals

Shows how optional inputs work under strict null checking. A value that
may be null never reaches a place that needs a value; the program writes
down what happens when it is null, and the compiler holds it to that.

What each piece demonstrates:

- **a declared default** — `suffix` is `optional(string, '!')`, so a
  missing or null value becomes `'!'` before anything reads it, and the
  checker treats it as a plain string.
- **the null test as the discharge** — `greeting` has no default, so the
  `banner` local tests it; in the else branch the checker knows the
  value is a string and lets it through. A conditional fits when the
  branches differ; for a plain fallback `??` is shorter.
- **`??` supplies a fallback** — the `upstreams` local lands a
  possibly-null list in one step, and the `label` fan-out iterates
  `var.labels ?? {}` so omitting the input means no instances.
- **`?.` rides a maybe** — `var.tls?.cert ?? 'self-signed'` reads
  through two optional levels and lands the result; each nullable
  level wears its own `?.`.
- **a filter narrows** — `ports` keeps each upstream's port only where
  `u.port != null` held, so the element type is integer, not
  optional(integer).
- **a constraint's when guards its require** — constraints read missing
  values as null, and the `when: var.upstreams != null` test narrows
  what `require` reads.

Try removing one of the discharges and recompiling: the error shows
the form to put back.

## Compile

From the unobin repo root:

```
go run ./cmd/unobin compile \
  -p examples/optionals/main.ub \
  -o /tmp/optionals-build \
  --replace-unobin="$(pwd)" \
  --build
```

## Run

```
cd /tmp/optionals-build
export UB_STATE_KEY=$(head -c 32 /dev/urandom | base64)
./optionals plan --allow-version-mismatch \
  -c "$OLDPWD"/examples/optionals/dev.ub -o /tmp/optionals-plan.json
./optionals apply /tmp/optionals-plan.json
./optionals output -c "$OLDPWD"/examples/optionals/dev.ub
```

The `label` resource fans out per entry of `labels`; rerun with the
`labels` line removed from `dev.ub` and the plan removes those files,
since the `?? {}` fallback made fan-out produce no instances. With no
`tls` in the config, the `cert` output reads 'self-signed' straight
through the `?.` chain.
