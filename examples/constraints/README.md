# constraints

Example that shows cross-field constraints at both layers: a UB
`constraints:` block on the stack's inputs, and Go-declared
constraints on a library resource type.

The stack imports a local Go library `deploy` whose `service`
resource renders a service spec to a file. A `std.fs-file` resource
writes a summary alongside it.

## The UB constraints block

The stack's `constraints:` entries check the operator's inputs at
plan time, before any work happens:

- `exactly-one-of` and `required-with` relate which top-level
  inputs are set: deploy from `image:` or `build:` but not both,
  and an image needs its `registry:`.
- A `[*]` field runs a set rule once per list element, so each
  replica's `cert` and `key` come together. A failure names the
  element that broke the rule (`input.replicas[0].cert`).
- A `predicate` checks a `when:`/`require:` pair and may call
  functions from the language namespace (`@core.length`).
- A predicate with `@for-each:` checks once per element, with
  `@each.key` and `@each.value` bound; a failure names the element
  (`input.replicas[1]`).

## The Go constraints

The `deploy` library's `Service` type declares its rules from a
`Constraints` method, referring to its own struct fields, so the Go
compiler checks that they exist and a rename updates them:

```go
func (s Service) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(s.Image, s.Build),
		constraint.Must(constraint.OneOf(s.Tier, "dev", "prod")).
			Message("tier must be dev or prod"),
		constraint.When(constraint.Equals(s.Tier, "prod")).
			Require(constraint.AtLeast(s.Replicas, 2)).
			Message("prod runs at least two replicas"),
		constraint.ForEach(s.Ports, func(p Port) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.When(constraint.IsTrue(p.TLS)).
					Require(constraint.Present(p.Cert)).
					Message("a tls port needs a cert"),
			}
		}),
	}
}
```

unobin reads the rules from source at compile time and checks every
`deploy.service` body against them with the same checker the UB
block uses, so the two layers speak one vocabulary. The stack rules
guard the operator's inputs at the boundary; the Go rules guard each
resource body, wherever its values come from. A rule both layers
care about (image or build, not both) can be declared at both.

## Compile

```
go run ./cmd/unobin compile \
  -p examples/constraints/factory.ub \
  -o /tmp/constraints-build \
  --build
```

## Run

```
cd /tmp/constraints-build
./constraints plan --allow-version-mismatch \
  -c "$OLDPWD"/examples/constraints/dev.ub -o /tmp/constraints-plan.json
./constraints apply /tmp/constraints-plan.json
./constraints output -c "$OLDPWD"/examples/constraints/dev.ub
```

The spec file renders as:

```
service app
tier: prod
replicas: 2
image: app:1.4.2
port 8443 tls cert=/etc/ssl/app.pem
port 9090
```

## Failures

Each rule rejects a bad `dev.ub` at plan time. Editing the inputs
one way at a time:

- Add `build: './src'` next to `image:`:
  `constraints[0] (exactly-one-of [input.image, input.build]): expected
  exactly one to be set, got 2 (input.image, input.build)`.
- Remove `registry:`:
  `constraints[1] (required-with): "input.image" is set, so
  [input.registry] must also be set; missing input.registry`.
- Remove the first replica's `key:`:
  `constraints[2] (required-together [input.replicas[0].cert,
  input.replicas[0].key]): expected all set or all null, got 1 set
  (input.replicas[0].cert)`.
- Remove the second replica while `tier: 'prod'`:
  `constraints[3] (predicate): prod runs at least two replicas`.
- Remove the first replica's `cert:` and `key:` while it has
  `tls: true`:
  `constraints[4] (predicate): a tls replica needs a cert
  (input.replicas[0])`.
- Set `tier: 'staging'`, which the stack rules allow but the Go
  rules reject, named relative to the resource body:
  `resource.app: schema: constraints[1] (predicate):
  tier must be dev or prod`.

A Go rule whose fields are written as literals in `factory.ub` fails at
compile, before there is a binary to plan with. Hardcoding both
`image: 'app:1.4.2'` and `build: './src'` in the `app` body:

```
Error: examples/constraints/factory.ub:66:25: schema: resource.app:
constraints[0] (exactly-one-of [image, build]): expected exactly one to be set,
got 2 (image, build)
```

Fields that read inputs (`tier: input.tier`) defer their rules to plan.
