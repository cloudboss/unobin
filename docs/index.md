# What is unobin

Unobin is a language and compiler for Infrastructure as Code (IaC). It is so named because [factory source](./authoring/factories.md) produces one binary executable that manages one or more [stacks](./authoring/stack-files.md).

A factory includes the Unobin runtime and all of its dependencies. Once the factory is compiled, it is the only thing you need to manage a stack. The `unobin` CLI is only needed during development.

<img src="assets/apply-ui.webp" alt="Apply UI showing an apply run" loading="eager">

The basic workflow is:

```
unobin generate factory -o appdeploy
cd appdeploy
# Edit factory.ub
unobin compile -o ./build --build --library-path github.com/example/appdeploy
./build/appdeploy schema template -o dev.ub
# Edit dev.ub
./build/appdeploy plan -c dev.ub -o plan.json.enc
./build/appdeploy apply --ui plan.json.enc
```

To learn how to build a factory, read the [getting started](./getting-started/installation.md) guide. Use the [language reference](./language/index.md) for `.ub` syntax. Read the [Go SDK](./go-sdk/index.md) guide if you want to write a Go library.

- [Build a first factory](getting-started/first-factory.md)
- [Compile, plan, and apply](getting-started/compile-plan-apply.md)
- [Unobin vs Terraform](overview/terraform.md)
