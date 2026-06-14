# configurations example

Shows how an operator routes per-deployment values to a library that declares a
configuration, how a composite remaps the alias for everything inside its
call, and how the factory defines a configuration of its own from a resource.

The stack defines three `greet.say` actions and two `greeter.greeting`
composite call sites. The leaves and call sites that omit a meta key use the
default alias; others select the `formal` alias either directly with
`@configuration:` or by remapping inside a composite with `@configurations:`.
The `fancy` configuration is internal: the factory derives its prefix from the
`resource.flourish` computed output, so it never appears in the stack file (an
operator entry for it is rejected), and the action that selects it runs after
the resource it derives from. A real library would use the same
mechanism to point one library at something another library created (a cluster
created here, configuring its own client library). Prefer configuration fields
that hold credential sources over short-lived secrets: the value is computed
once per run, so a token that expires in minutes belongs behind a field the
library exchanges per call.

## Try it

```
go run ./cmd/unobin compile \
  -p examples/configurations/factory.ub \
  -o /tmp/configurations-build \
  --replace-unobin="$(pwd)" \
  --build

cd /tmp/configurations-build
export UB_STATE_KEY="$(head -c 32 /dev/urandom | base64)"
./configurations plan --allow-version-mismatch \
  -c "${OLDPWD}/examples/configurations/dev.ub" \
  -o /tmp/plan.json
./configurations apply /tmp/plan.json
./configurations output -c "${OLDPWD}/examples/configurations/dev.ub"
```

Expected output:

```
casual:      "hello: world"
casual-wrap: "hello: wrapped"
fancy:       "** Salutations **: world"
formal:      "Good day: world"
formal-wrap: "Good day: wrapped"
```

## Failures

The plan-time validator catches misuse before any work happens:

- Mistype an alias: `@configuration: greet.formel` produces
  `@configuration greet.formel: configuration not declared`.
- Remove the `default` entry while something still uses it: the node that
  does reports `library "greet" requires a configuration; define
  greet.default under factory.configurations in the stack file or under
  configurations in the factory`.
- Supply a value for an internal name: `factory.configurations.greet.fancy`
  in the stack file produces `defined internally by the factory; remove this
  entry from the stack file`.
- Cross-import remap: `@configurations: { greet: aws.formal }` produces
  `@configurations.greet: right-hand side import "aws" must match the key`.
