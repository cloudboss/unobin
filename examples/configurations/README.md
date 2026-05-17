# configurations example

Shows how an operator routes per-deployment values to a module that declares a
configuration, and how a composite remaps the alias for everything inside its
call.

The stack defines two `greet.say` actions and two `greeter.greeting`
composite call sites. The leaves and call sites that omit a meta key use the
default alias; the others select the `formal` alias either directly with
`@configuration:` or by remapping inside a composite with `@configurations:`.

## Try it

```
go run ./cmd/unobin compile \
  -p examples/configurations/stack.ub \
  -o /tmp/configurations-build \
  --replace-unobin="$(pwd)" \
  --unobin-version=v0.0.0 \
  --build

cd /tmp/configurations-build
export UB_STATE_KEY="$(head -c 32 /dev/urandom | base64)"
./configurations plan --allow-version-mismatch \
  -c "${OLDPWD}/examples/configurations/dev.ub" \
  -o /tmp/plan.json
./configurations apply /tmp/plan.json
./configurations output
```

Expected output:

```
casual:      "hello: world"
casual-wrap: "hello: wrapped"
formal:      "Good day: world"
formal-wrap: "Good day: wrapped"
```

## Failures

The plan-time validator catches misuse before any work happens:

- Mistype an alias: `@configuration: greet.formel` produces
  `@configuration greet.formel: alias not declared in configurations`.
- Drop a `default` entry: `configurations.greet: missing default entry`.
- Cross-import remap: `@configurations: { greet: aws.formal }` produces
  `@configurations.greet: right-hand side import "aws" must match the key`.
