# Unobin

Unobin means _one binary_. It's a tool for infrastructure automation inspired by [Terraform](https://developer.hashicorp.com/terraform), [Ansible](https://docs.ansible.com/projects/ansible/latest/index.html), and others, but unlike those, unobin compiles your code to a standalone binary called a factory.

## Quickstart

Install unobin:

```
go install github.com/cloudboss/unobin/cmd/unobin@latest
```

To start a new factory, use the `unobin generate factory` command.

```
unobin generate factory -o appdeploy
```

Now you will have a new directory `appdeploy` (given by `-o`) containing a
`factory.ub` file. Edit `factory.ub` to import libraries and add resources.

When you compile, give it a library path with `--library-path`. This is similar to Go's `module-path` when running `go mod init`. It will normally be the git repo where your library will live.

In the `appdeploy` directory, run:

```
unobin compile -o ./appdeploy-compiled --build --library-path github.com/cloudboss/mystack
```

Now there will be an executable called `./appdeploy-compiled/appdeploy`. You
can use it to generate a stack file from the factory's input schema:

```
./appdeploy-compiled/appdeploy schema template -o dev.ub
```

Edit the generated `dev.ub` if necessary.

Then run plan and apply. A factory cannot apply without first planning.

```
./appdeploy-compiled/appdeploy plan -o plan.json -c dev.ub
./appdeploy-compiled/appdeploy apply plan.json
```

See the [examples](./examples) directory for various example stacks that you can compile and run.

## Dependency projects and import packages

A dependency project is a versioned directory with `manifest.ub` or `go.mod` at
its root. A package import may name any directory below that project:

```ub
imports: {
  helloer: 'github.com/acme/repo//ub/helloer'
}
```

The manifest names the owning project, not every package below it:

```ub
manifest: {
  requires: { 'github.com/acme/repo': 'v1.2.3' }
}
```

`unobin deps get` adds projects. If a repository subdirectory has its own
`manifest.ub` or `go.mod`, it is a project and may be added directly:

```ub
manifest: {
  requires: { 'github.com/acme/repo//library-c': 'v1.2.3' }
}
```

The repository root uses ordinary semver tags such as `v1.2.3`. A project in a
subdirectory uses tags prefixed by that project path, such as `library-c/v1.2.3`
or `libs/core/v1.2.3`. Package paths below the project do not change the tag.

A nested `manifest.ub` is a project boundary. `unobin deps sync` for an ancestor
project does not scan files under that nested project. Run
`unobin deps sync -p library-c` to manage `library-c/manifest.ub` and
`library-c/lock.ub`.

Use a project id plus a replacement for local development against a nested
project:

```ub
manifest: {
  requires: { 'github.com/acme/repo//library-c': 'v1.2.3' }
  replace:  { 'github.com/acme/repo//library-c': './library-c' }
}
```

Relative imports may only target source governed by the same nearest
`manifest.ub`.

## Benefits of Unobin

### No Dependencies

An unobin factory includes the runtime and dependencies. It's like having your modules, providers, and Terraform itself all included in one executable.

### Consistent Interface

All factories have the same command line arguments with automatically generated help. If you know how to run one, you know how to run all of them.

### Reproducible

The goal is: if it works on my machine, then it works on your machine. You don't need to do extra steps or install anything before you can deploy your infrastructure. Just download the factory and run it.

### Input Validation

All factories validate their inputs against a schema and will not run if the inputs do not pass validation.

## Comparison with Other Tools

|                      | Unobin | Ansible    | Chef   | Terraform  |
|:--------------------:|:------:|:----------:|:------:|:----------:|
|No server             |&check; |            |        |&check;     |
|Local mode            |&check; |optional    |&check; |&check;     |
|Syntax                |Unobin  |YAML+Jinja2 |Ruby    |HCL         |
|Works on my machine   |&check; |maybe       |maybe   |maybe       |
|Works on your machine |&check; |maybe       |maybe   |maybe       |
