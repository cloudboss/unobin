# for-each

Example that shows `@for-each` on a resource, an action, and a
composite call site, all iterating the same `var.files` map.

The stack imports a local UB library `notify` that exports a single
composite type `alert`. Each iteration produces:

- a `std.fs-file` resource holding the message text
- a `notify.alert` composite that writes a second file with an
  `ALERT topic: body` line (the composite itself wraps `std.fs-file`)
- a `std.exec-command` action that echoes the per-instance file path
  and its sha256

The action references the resource's per-instance output through
`resource.file[@each.key]`. On a fresh apply that reference is unknown
at plan time, so the plan shows the action with empty inputs and apply
re-evaluates the body against the live scope.

Data sources also support `@for-each` with the same semantics, but no
built-in library ships one for this example.

## Compile

```
go run ./cmd/unobin compile \
  -p examples/for-each/factory.ub \
  -o /tmp/for-each-build \
  --replace-unobin="$(pwd)" \
  --build
```

## Run

```
cd /tmp/for-each-build
./for-each plan --allow-version-mismatch \
  -c "$OLDPWD"/examples/for-each/dev.ub -o /tmp/for-each-plan.json
./for-each apply /tmp/for-each-plan.json
./for-each output
./for-each state list
```

The plan output groups the leaf instances under their template:

```
> action.std.exec-command.announce  (for-each, 2 instances)
  > ['alpha']
  > ['beta']
+ resource.std.fs-file.file  (for-each, 2 instances)
  + ['alpha']
      content: "first message"
      path: "/tmp/unobin-for-each/alpha.txt"
  + ['beta']
      content: "second message"
      path: "/tmp/unobin-for-each/beta.txt"
+ resource.notify.alert.alert['alpha']  (library notify.alert)
    body: "first message"
    path: "/tmp/unobin-for-each/alpha.alert"
    topic: "alpha"
  + std.fs-file.this
      content: "ALERT alpha: first message\n"
      path: "/tmp/unobin-for-each/alpha.alert"
+ resource.notify.alert.alert['beta']  (library notify.alert)
    ...
```

Composite instances render as their own subtree per instance, with
the boundary header carrying the per-instance address and the
internals nested under it. Leaf for-each instances (the resource
and the action) group under one template header.

State after apply contains, per instance:

- `resource.std.fs-file.file['<key>']` — a leaf entry per file
- `resource.notify.alert.alert['<key>']` — a library-call entry per
  composite instance
- `resource.notify.alert.alert['<key>']/std.fs-file.this` — the leaf
  inside each composite instance
- `action.std.exec-command.announce['<key>']` — an action entry per
  echo

Removing a key from `files:` in `dev.ub` and re-planning destroys
both the leaf and the composite-internal leaf for the removed key.
