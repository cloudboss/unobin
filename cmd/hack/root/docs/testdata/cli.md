# CLI reference

Compile and manage unobin stacks

## unobin check

Check Unobin source without compiling it

```
unobin check [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--format string` | `text` | Output format: text, json, unobin. |
| `-p, --path string` | `.` | Path to a Unobin source file or directory. |
| `--replace-unobin string` |  | Local path to substitute for github.com/cloudboss/unobin so schema checks read it. |

## unobin compile

Generate a factory binary's main.go from factory source

```
unobin compile [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--build` | `false` | After writing the source, run `go build` in the output directory. |
| `--format string` | `text` | Output format: text, json, unobin. |
| `--go-version string` | `1.26` | Go toolchain version to declare in the generated go.mod. |
| `--library-path string` |  | Library path identity to embed in the binary. The operator's stack file asserts the same value under factory.pin.library-path and plan, refresh, and validate refuse on mismatch. |
| `--name string` |  | Stack name. Defaults to the parent directory's basename. |
| `-o, --out string` |  | Directory to write main.go and go.mod into, or `-` to print main.go to stdout. |
| `-p, --path string` | `.` | Path to the factory source file or directory. |
| `--replace-go-module stringArray` | `[]` | Local replace for a Go module, repeatable. Format: `module-path=local-path`. Both the import resolver and the generated go.mod use the substitution. |
| `--replace-unobin string` |  | Local path to substitute for github.com/cloudboss/unobin via a go.mod replace directive. |
| `--version string` | `v0.0.0` | Release version to stamp into the built binary. |

## unobin deps

Manage dependency floors in project.ub and selected versions in project-lock.ub.

A factory or UB library writes imports in .ub source. The project records
its direct dependency floors, and project-lock records the versions and source
hashes the compiler should use.

### unobin deps clean

Remove the cached dependency sources

```
unobin deps clean [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--format string` | `text` | Output format: text, json, unobin. |

### unobin deps get

Add or update a dependency floor and re-pin

```
unobin deps get <dependency>[@version] [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--format string` | `text` | Output format: text, json, unobin. |
| `-p, --path string` | `.` | Path to the factory source file or project directory. |
| `--replace-unobin string` |  | Local path to substitute for github.com/cloudboss/unobin so the resolver reads from a working tree instead of fetching. |

### unobin deps list

List the project-lock dependencies

```
unobin deps list [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--format string` | `text` | Output format: text, json, unobin. |
| `-p, --path string` | `.` | Path to the factory source file or project directory. |

### unobin deps sync

Reconcile the project and project-lock with the imports

```
unobin deps sync [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--format string` | `text` | Output format: text, json, unobin. |
| `-p, --path string` | `.` | Path to the factory source file or project directory. |
| `--replace-unobin string` |  | Local path to substitute for github.com/cloudboss/unobin so the resolver reads from a working tree instead of fetching. |

### unobin deps verify

Check the cached dependencies against project-lock

```
unobin deps verify [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--format string` | `text` | Output format: text, json, unobin. |
| `-p, --path string` | `.` | Path to the factory source file or project directory. |
| `--replace-unobin string` |  | Local path to substitute for github.com/cloudboss/unobin so the resolver reads from a working tree instead of fetching. |

## unobin fmt

Reformat one or more .ub files in canonical form.

With no path arguments, fmt reads from stdin and writes the
formatted bytes to stdout. With one or more paths, each file is
formatted in turn; directory arguments are walked recursively for
*.ub files.

Flags:
  -w / --write   overwrite each file in place instead of writing to
                 stdout.
  -l / --list    print the names of files whose formatted output
                 differs from their current contents; no other output.

Examples:
  unobin fmt factory.ub
  unobin fmt -w factory.ub libraries/
  unobin fmt -l .

```
unobin fmt [paths...] [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `-l, --list` | `false` | Print paths whose formatted output differs from their contents. |
| `--max-line-length int` | `100` | Target line width for formatted output. |
| `--wrap-strings` | `false` | Rewrite overflowing single-quoted strings as folded or joined triple-quoted form. |
| `-w, --write` | `false` | Write the formatted output back to the source file. |

## unobin generate

Generate code and scaffold libraries

### unobin generate factory

Scaffold a new factory directory.

The generated directory contains a factory.ub source file with empty
placeholder blocks the author fills in. A stack file is operator supplied
per stack; use the compiled factory's schema template command to create
one.

Examples:
  unobin generate factory -o ./my-factory

```
unobin generate factory [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--force` | `false` | Overwrite files if the output directory already exists |
| `--format string` | `text` | Output format: text, json, unobin. |
| `-o, --output string` |  | Output directory for the generated factory |

### unobin generate golibrary

Generate a Go library from a Terraform provider schema.

The generated Go library contains typed structs with ub tags and
CRUD method stubs for every resource in the provider.

Examples:
  unobin generate golibrary --from tf --provider random --go-module-path example.com/libraries/random
  unobin generate golibrary --from tf --provider aws -o ./aws-library --go-module-path example.com/libraries/aws

```
unobin generate golibrary [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--format string` | `text` | Output format: text, json, unobin. |
| `--from string` | `tf` | Schema source |
| `--go-module-path string` |  | Go module path for go.mod (e.g., example.com/libraries/aws) |
| `-o, --output string` |  | Output directory for the generated Go library |
| `--provider string` |  | Terraform provider source (e.g., hashicorp/aws, ansible/ansible) |
| `--provider-version string` |  | Terraform provider version constraint (e.g., "~> 5.0") |
| `--replace-unobin string` |  | Local path to substitute for github.com/cloudboss/unobin via a go.mod replace directive |

### unobin generate ublibrary

Scaffold a new UB library directory.

The generated directory contains one starter resource composite export
file named <type>.ub. The directory listing is the project, so there is
no separate project file. Blocks are empty for the author to fill in.

Examples:
  unobin generate ublibrary -o ./greeter
  unobin generate ublibrary -o ./greeter --type greeting

```
unobin generate ublibrary [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--force` | `false` | Overwrite files if the output directory already exists |
| `--format string` | `text` | Output format: text, json, unobin. |
| `-o, --output string` |  | Output directory for the generated library |
| `--type string` | `example` | Name of the initial composite type to export |

## unobin lsp

Run the Unobin language server over stdio

```
unobin lsp [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--log string` |  | write server log |
| `--trace string` |  | write JSON-RPC trace log |

## unobin print-graph

Print a factory's dependency graph from its source.

Imports are resolved in memory; composite call sites are expanded
into their internal sub-nodes the same way the generated binary's
print-graph subcommand does. The output is intended to match what
the compiled binary would emit.

Examples:
  unobin print-graph
  unobin print-graph -p factory.ub --format dot | dot -Tsvg > graph.svg

```
unobin print-graph [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--format string` | `text` | Output format: text, json, unobin, dot. |
| `-p, --path string` | `.` | Path to the factory source file or directory. |
| `--replace-unobin string` |  | Local path to substitute for github.com/cloudboss/unobin so the resolver reads from a working tree. |

## unobin version

Print the unobin version

```
unobin version [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--format string` | `text` | Output format: text, json, unobin. |
