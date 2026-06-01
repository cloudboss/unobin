# splat

Shows off splat expressions. The stack takes a list of subnets and a list
of zones as input, then uses `[*]` to read fields out of them and writes a
summary file.

A splat is shorthand for a `for` comprehension: `var.subnets[*].id` reads
the `id` of every subnet, exactly like `[ for s in var.subnets : s.id ]`.
Everything to the right of the `[*]` applies to each element, and the
result is a list.

What each piece demonstrates:

- **field projection** — `ids`, `cidrs`, and `public` each read one field
  from every subnet.
- **the long way** — `ids-the-long-way` produces the same list as `ids`
  using a comprehension, so you can see the two side by side.
- **nested splat** — `host-ips` projects twice: every zone, and within
  each zone every host's `ip`, giving a list of lists.

The resource `content` also feeds a splat through `format` to list the
subnet ids in the summary file. A splat must end in a field; a bare
`var.subnets[*]` is rejected, since it would just be the list itself.

## Compile

From the unobin repo root:

```
go run ./cmd/unobin compile \
  -p examples/splat/main.ub \
  -o /tmp/splat-build \
  --replace-unobin="$(pwd)" \
  --unobin-version=v0.0.0 \
  --build
```

## Run

```
cd /tmp/splat-build
export UB_STATE_KEY=$(head -c 32 /dev/urandom | base64)
./splat plan --allow-version-mismatch \
  -c "$OLDPWD"/examples/splat/dev.ub -o /tmp/splat-plan.json
./splat apply /tmp/splat-plan.json
./splat output -c "$OLDPWD"/examples/splat/dev.ub
```
