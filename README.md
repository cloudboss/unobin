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

Now you will have a new directory `appdeploy` (given by `-o`) containing a `factory.ub` file. Edit `factory.ub` to import modules and add resources.

When you compile, give it a library path with `--library-path`. This is similar to Go's `module-path` when running `go mod init`. It will normally be the git repo where your library will live.

In the `appdeploy` directory, run:

```
unobin compile -o ./appdeploy-compiled --build --library-path github.com/cloudboss/mystack
```

Now there will be an executable called `./appdeploy-compiled/appdeploy`. You can use it to generate a configuration file from the stack's input schema:

```
./appdeploy-compiled/appdeploy schema template -o config.ub
```

Edit the generated `config.ub` if necessary.

Then run plan and apply. A factory cannot apply without first planning.

```
./appdeploy-compiled/appdeploy plan -o plan.json -c config.ub
./appdeploy-compiled/appdeploy apply plan.json
```

See the [examples](./examples) directory for various example stacks that you can compile and run.

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
