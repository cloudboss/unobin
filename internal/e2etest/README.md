# e2etest

`internal/e2etest` is the required framework for Unobin tests whose behavior is
visible through commands, source roots, or compiled factory binaries. Use it
instead of source-heavy Go tests when the result can be checked through command
output, generated files, plan summaries, state summaries, or state/plan envelope
metadata.

Use `internal/ubtest` instead for syntax-only parsing, formatting, and
diagnostic tests over `.ub` fixtures.

## Test entry points

`tests/e2e` owns the black-box suites:

```go
func TestCompiledCases(t *testing.T) {
    e2etest.RunCompiledCases(t, "testdata/compiled-cases")
}

func TestSourceCases(t *testing.T) {
    e2etest.RunSourceCases(t, "testdata/source-cases")
}
```

The harness discovers every `case.json` below the supplied directory and runs one
subtest per case.

## Compiled factory cases

Use compiled cases when the behavior belongs to a generated factory binary:
`validate`, `schema`, `print-graph`, `plan`, `apply`, `output`, `refresh`, state
commands, plan files, state files, effects, masking, lifecycle behavior, library
config, inputs, constraints, composites, and for-each behavior.

Fixture layout:

```text
tests/e2e/testdata/compiled-cases/<case>/
  case.json
  src/
    factory.ub
    project.ub
    project-lock.ub
    libraries/
      ...
  stacks/
    dev.ub
  files/
    ...
  want/
    command.stdout
    command.stderr
    output.json
    plan-summary.json
    state-summary.json
```

A minimal case:

```json
{
  "name": "minimal",
  "factoryPath": "src",
  "libraryPath": "example.com/unobin/e2e/minimal",
  "build": true,
  "commands": [
    {
      "name": "validate",
      "args": ["validate", "-c", "stacks/dev.ub"],
      "stdout": "want/validate.stdout",
      "stderr": "want/validate.stderr"
    },
    {
      "name": "plan-create",
      "args": ["plan", "--ascii", "-c", "stacks/dev.ub", "-o", "plan.ubp"],
      "stdout": "want/plan-create.stdout",
      "stderr": "want/plan-create.stderr"
    }
  ],
  "planSummaries": [
    { "path": "plan.ubp", "want": "want/plan-summary.json" }
  ],
  "stateSummary": "want/state-summary.json"
}
```

Compiled case fields:

- `name`: subtest name. Must be unique.
- `factoryPath`: source root, relative to the case directory.
- `libraryPath`: generated factory module path.
- `build`: when true, build and execute the generated binary.
- `commands`: commands run against the compiled binary.
- `files`: workspace files compared to goldens after all commands.
- `absentFiles`: workspace files that must not exist after all commands.
- `planSummaries`: plan files decoded into stable JSON summaries.
- `planEnvelopes`: plan envelope metadata compared to goldens.
- `stateEnvelopes`: current state envelope metadata compared to goldens.
- `stateSummary`: stable state JSON summary for the last stack file used.
- `stateSeed`: seed state summary to install before commands run.
- `extraStateSnapshots`: extra prior snapshots to create from `stateSeed`.
- `stateLocks`: stack files whose state lock should exist before commands run.
- `deterministic`: marks cases intended to be deterministic.

The harness compiles through `compile.Run` with local replacements for this
checkout and `tests/e2e/testdata/modules/e2elib`. It copies each case to a temp
workspace before execution, so tests must refer to case files by relative paths.

Commands that use `-c` or `--config` are pinned automatically before first use.
Set `skipPin: true` only for cases that are testing pin errors or pin mismatch
behavior.

## Source-root cases

Use source cases when the behavior belongs to the Unobin CLI before a compiled
factory binary exists: `deps`, `compile`, `generate`, source-root `print-graph`,
fake remotes, dependency files, generated `go.mod`, generated `main.go`, and
source diagnostics.

Fixture layout:

```text
tests/e2e/testdata/source-cases/<case>/
  case.json
  root/
    factory.ub
    project.ub
    project-lock.ub
    libraries/
      ...
  remotes/
    ...
  want/
    stdout
    stderr
    project.ub
    project-lock.ub
    go.mod
    main.go
```

A source case can execute either a compiled `unobin` process or the root command
in-process:

```json
{
  "name": "compile-reference-errors",
  "rootPath": ".",
  "executor": "root",
  "cliVersion": "v0.1.0",
  "commands": [
    {
      "name": "unknown-go-type",
      "dir": "unknown-go-type",
      "args": ["compile", "-p", "factory.ub", "-o", "build"],
      "stderr": "want/unknown-go-type.stderr",
      "exitCode": 1
    }
  ]
}
```

Source case fields:

- `name`: subtest name. Must be unique.
- `rootPath`: default command directory relative to the case directory.
- `executor`: `process` builds and runs the CLI. `root` runs the Cobra command
  in-process with fake resolvers.
- `cliVersion`: version string used by the in-process executor.
- `build`: skip under `testing.Short()` when the case invokes Go builds.
- `remotes`: fake remote source roots keyed by URL, subdir, and version.
- `tags`: fake tag results for dependency commands.
- `commands`, `files`, and `absentFiles`: same comparison model as compiled
  cases.

Source cases get a copy of `tests/e2e/testdata/modules/e2elib` at
`modules/e2elib` in the workspace when that target does not already exist.
Command args and env values can use `$WORKSPACE`, `$REPO_ROOT`, and
`$E2E_LIBRARY_DIR`.

## Command checks and goldens

Each command has these fields:

- `name`: label used in failures.
- `args`: command arguments, excluding the executable path.
- `dir`: working directory relative to the workspace.
- `env`: extra environment variables.
- `stdout` and `stderr`: golden paths relative to the case directory.
- `exitCode`: expected exit code. Omit for success.
- `skipPin`: compiled cases only; disables automatic stack pinning.
- `tamperPlanFiles`: compiled cases only; flips plan bytes before the command.

Compare complete stdout and stderr. Do not use substring assertions when a whole
file golden can describe the behavior. Empty output may omit the golden path.
When `-update` is active, empty goldens are removed.

Files are compared through `files`:

```json
{
  "files": [
    { "path": "work/events.ndjson", "want": "want/events.ndjson" }
  ]
}
```

Dynamic values should be normalized narrowly in the harness or in the fixture
script that creates them. Do not normalize broad text patterns that could hide a
real diagnostic change.

## Updating and running

Update one case:

```text
go test ./tests/e2e -run 'TestCompiledCases/minimal$' -update -count=1
go test ./tests/e2e -run 'TestCompiledCases/minimal$' -count=1
```

Run source cases the same way:

```text
go test ./tests/e2e -run 'TestSourceCases/compile-reference-errors$' -count=1
```

Use `-short` to skip build-heavy cases. Before commit, run the relevant narrow
test, then the broader package or suite. For harness changes, also run:

```text
go test ./internal/e2etest -count=1
```

## Adding a case

1. Pick compiled or source based on the behavior being tested.
2. Put all `.ub` source in fixture files, not Go string literals.
3. Add `case.json` with commands and expected artifacts.
4. Add empty or missing goldens, then run with `-update`.
5. Inspect every generated golden before committing.
6. Run the same command without `-update`.

Prefer one case with a meaningful command sequence over many tiny Go tests that
repeat the same source root. Avoid network access and machine-specific absolute
paths in every e2e case.
