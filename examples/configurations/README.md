# configurations example

Shows how an operator routes per-deployment values to a library that declares a
configuration, how a stack file replaces a factory-declared configuration body,
how a composite remaps the alias for everything inside its call, and how the
factory defines a configuration of its own from a resource.

The factory defines four `greet.say` actions and two `greeter.greeting`
composite call sites. The leaves and call sites that omit a meta key use the
default configuration; others select `configuration.formal` either directly
with `@configuration:` or by remapping inside a composite with
`@configurations:`.

The `formal` and `fancy` configurations are declared in `factory.ub` with empty
bodies. The `dev.ub` stack file replaces those bodies and supplies the required
`prefix`. If `dev.ub` removes the `fancy` entry, the factory body becomes
effective and plan reports the missing required field.

The `derived` configuration stays in the factory: it derives its prefix from
the `resource.flourish` computed output, and the action that selects it runs
after the resource it derives from. A real library would use the same mechanism
to point one library at something another library created. Prefer configuration
fields that hold credential sources over short-lived secrets: the value is
computed once per run, so a token that expires in minutes belongs behind a field
the library exchanges per call.

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
casual:      'hello: world'
casual-wrap: 'hello: wrapped'
derived:     '** Salutations **: world'
fancy:       'Stack says: world'
formal:      'Good day: world'
formal-wrap: 'Good day: wrapped'
```

## Failures

The plan-time validator catches misuse before any work happens:

- Mistype a configuration name: `@configuration: configuration.formel` produces
  `@configuration configuration.formel: configuration not declared`.
- Remove the `greet { ... }` entry while something still uses it: the node that
  does reports `library "greet" requires a configuration; add greet { ... }
  under stack.factory.configurations or configurations in the factory`.
- Remove the `fancy` entry from `dev.ub`: the factory body is used and plan
  reports `field prefix: required`.
- Cross-import remap: `@configurations: { greet: configuration.aws-formal }`
  produces `@configurations.greet: right-hand side import "aws" must match the
  key`.
