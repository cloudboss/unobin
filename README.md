# Unobin

Unobin means _one binary_. It's a tool for cloud automation inspired by [Ansible](https://github.com/ansible/ansible), but unlike Ansible, an unobin playbook compiles to a standalone binary.

## Quickstart

First get unobin:

```
GO111MODULE=on go get -u github.com/cloudboss/unobin/unobin
```

To start a new playbook project, you will use the `unobin init` command. Unobin playbooks compile to go, so in addition to giving the project a name, you will also need to give it a Go import path. This is usually the name of the Git repository where you will host the project.

Start the project, giving it the project name with `-p` and Go import path with `-i`:

```
unobin init -p myplaybook -i github.com/cloudboss/myplaybook
```

Now you will have a directory created with the project name, containing an example playbook `playbook.ub`, a Go module definition `go.mod`, and a `resources` directory containing a template. Change to the project directory and compile:

```
cd myplaybook
unobin compile -p playbook.ub
```

Now there will be an executable called `playbook` with the contents of `resources` compiled into it. To run the playbook, you need some input variables. These are written in JSON. For the example playbook you only need one variable called `name`:

```
echo '{"name": "MyName"}' > vars.json
./playbook apply -v vars.json
```

## Benefits of Unobin

### No Dependencies

An unobin playbook includes the runtime. It's like having Python, Ansible, and all dependencies included in the playbook itself.

### Consistent Interface

All playbooks have the same command line arguments with automatically generated help. If you know how to run one, you know how to run all of them.

### Reproducible

The goal is: if it works on my machine, then it works on your machine. You don't need to do extra steps or install anything before you can run a playbook. Just download the binary and run it.

### Serverless

No, not _that_ kind of serverless. It means unobin playbooks don't need to connect to a server or run from a control node. You run a playbook where you need it, whether from CI/CD or an individual machine that runs it to configure itself. The only "server" you need is for storage to host the playbook binary; use [Artifactory](https://jfrog.com/artifactory/), [Nexus](https://www.sonatype.com/nexus/repository-oss), a cloud storage bucket, or bake it into an image so it's already there when your machines boot.

### Predictable

There is one source for input variables: they are passed in as an argument to the playbook. Unlike Ansible, there are no levels of [precedence for variables](https://docs.ansible.com/ansible/latest/user_guide/playbooks_variables.html#understanding-variable-precedence).

There is only one pass through the playbook. There are no lookup plugins to run at a different time from the task where they are called. In unobin, "lookups" are just ordinary tasks that produce output. The templating language is minimal, may only be used for task arguments, and is evaluated at the beginning of each task's execution.

### Input Validation

All playbooks validate their input variables against a [schema](https://json-schema.org/). The playbook will only run if the inputs pass validation.

## Comparison with Other Tools

|                      | Unobin | Ansible    | Chef   |
|:--------------------:|:------:|:----------:|:------:|
|No server             |&check; |            |        |
|Local mode            |&check; |optional    |&check; |
|Syntax                |Unobin  |YAML+Jinja2 |Ruby    |
|Works on my machine   |&check; |maybe       |maybe   |
|Works on your machine |&check; |maybe       |maybe   |
