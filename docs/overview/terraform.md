# Unobin vs Terraform

Unobin and Terraform both compute plans and apply them against stored state. They make different choices about packaging and execution.

| Area | Unobin | Terraform |
| --- | --- | --- |
| Deliverable | Compiled factory executable | CLI plus modules plus providers |
| Dependency management | Compile-time with all dependencies included in every factory | Runtime download and caching of providers and modules |
| Language | [Unobin](../language/index.md) | HCL |
| Reuse unit | Library | Module and provider |
| Runtime | Included in the factory | External CLI |
| State | Factory reads and writes configured state | Terraform CLI reads and writes configured state |
| Distribution | Distribute the factory binary | Distribute modules plus provider and toolchain requirements |
| User interface | CLI with realtime visualization in browser during apply | CLI |

In Unobin, source is compiled into a factory. The factory is run against a configuration, called a stack file. The factory validates the stack file, computes a plan, writes a plan file, and applies that plan. Factories always plan before applying; there is no skipping the plan.

In Terraform, you run the Terraform CLI against a root module. At execution time, the CLI downloads providers and additional modules if defined. Terraform can apply without planning first.

Unobin defines one unit of reuse: the library. Libraries can be written in either Unobin or Go. Both kinds are called the same way in `.ub` code, and are compiled and included in the factory binary.

Terraform defines two units of reuse: modules and providers. Modules are `.tf` (HCL) files interpreted at runtime, and downloaded at runtime if they are defined externally. Providers are written in Go, run as separate processes, and communicate with the Terraform runtime over gRPC. Both are dynamic runtime systems and are called differently in code.

Unobin maintains a strict separation between configuration and code that performs work. Stack files cannot contain arbitrary code, only configuration and factory inputs.

In Terraform, a module is a module is a module. Where does the configuration go? Anywhere. Where does it end? Nowhere. You can just add another module on top, or use an additional tool like Terragrunt to try to end it once and for all.
