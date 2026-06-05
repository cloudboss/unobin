# comprehensions

Shows off if-expressions and list/map comprehensions. The stack takes a
list of services as input and transforms it every way the language
allows, then writes a summary file and exposes the transformations as
outputs.

What each piece demonstrates:

- **if-expression** — `replicas` picks `3` in prod and `1` otherwise.
- **list comprehension with a filter** — `public-names` keeps the names
  of the services whose `public` flag is set.
- **map comprehension (index-by)** — `ports-by-name` keys each service's
  port by its name.
- **group-by** — `names-by-region` collects the names under each region
  using the `...` form.
- **if-expression inside a comprehension** — `tiers` labels each service
  `public` or `internal`.
- **nested comprehension** — `by-region` walks the regions and, for each,
  runs an inner comprehension over the services in that region.
- **comprehension inside an if-expression** — `prod-names` returns every
  name in prod and an empty list elsewhere.

A `locals:` block computes `replicas` and `public-names` once for the
resource `content` and the outputs to share; the content splices them
into an interpolated string, joining the names with `@core.join`.

## Compile

From the unobin repo root:

```
go run ./cmd/unobin compile \
  -p examples/comprehensions/main.ub \
  -o /tmp/comprehensions-build \
  --replace-unobin="$(pwd)" \
  --unobin-version=v0.0.0 \
  --build
```

## Run

```
cd /tmp/comprehensions-build
export UB_STATE_KEY=$(head -c 32 /dev/urandom | base64)
./comprehensions plan --allow-version-mismatch \
  -c "$OLDPWD"/examples/comprehensions/dev.ub -o /tmp/comprehensions-plan.json
./comprehensions apply /tmp/comprehensions-plan.json
./comprehensions output -c "$OLDPWD"/examples/comprehensions/dev.ub
```
