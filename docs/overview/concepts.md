# Core concepts

The following terminology is used throughout Unobin.

## Factory

A factory is the compiled executable produced from a `factory.ub`. It contains the Unobin runtime, library dependencies, and the factory body.

## Stack

A stack is one managed instance of a factory. One factory can manage multiple stacks. The stack file defines the state backend, encryption key source, and factory inputs.

## Category

A category is an action, data source, or resource.

An action does imperative tasks and optionally returns output.

A data source does readonly lookups of external sources and returns output.

A resource does full CRUD operations on cloud or other external sources.

## Kind

A kind is a library's implementation of a category.

In the following example, `iam-openid-connect-provider` is a data source kind in the `aws` library.

```
data-sources: {
  github: aws.iam-openid-connect-provider {
    url: input.oidc-provider-url
  }
}
```

## Composite

A composite kind is written in `.ub`, as opposed to primitive kinds, which are written in Go. Composites combine other kinds behind a declared input and output interface.

## Library

A library can be imported and provides kinds and functions.

A library written in `.ub` always provides composite kinds. A Go library implements primitive kinds and functions. Functions can only be written in Go.

## Project

A project is the versioned dependency unit inside a git repository. A repository root can be a project, and a subdirectory can also be a project if it has its own `project.ub` or `go.mod`.

`project.ub` defines direct dependency requirements and local replacements.

`project-lock.ub` defines the selected dependency set used by `compile`.

## State backend and encryption

State is stored in a backend and is encrypted. The configuration for state backends and encryption is done in the stack file. Plan files are also encrypted using the same encryption configuration.
