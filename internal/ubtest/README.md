# ubtest

`internal/ubtest` is the required framework for Unobin syntax-only tests over
`.ub` fixtures. Use it for parser diagnostics, syntax validation, formatting,
canonical rendering, type expression parsing, import extraction, and other tests
where a `.ub` file plus `.ub.err` or `.ub.out` golden fully describes the
expected result.

Use `internal/e2etest` instead when behavior is visible through the CLI or a
compiled factory binary.

## Import path

```go
import "github.com/cloudboss/unobin/internal/ubtest"
```

The package lives under `internal` because it is test infrastructure for this
repo, not a public API.

## Fixture layout

All `.ub` test fixtures must live below a package-local `testdata/ub` tree and
must include a `valid` or `invalid` path segment:

```text
pkg/lang/testdata/ub/format/valid/list.ub
pkg/lang/testdata/ub/format/valid/list.ub.out
pkg/lang/parse/testdata/ub/invalid/unclosed-array.ub
pkg/lang/parse/testdata/ub/invalid/unclosed-array.ub.err
```

Rules:

- `valid` fixtures usually have no `.ub.err` golden.
- `invalid` fixtures use a matching `.ub.err` golden.
- Formatter or renderer fixtures use `.ub.out` for expected output.
- A fixture may have both `.ub.err` and `.ub.out` if the driver returns both.
- Do not put `.ub` fixtures outside `testdata/ub`.
- Do not add inline `.ub` Go string literals unless the assertion cannot be
  expressed with a fixture.

## Basic usage

A ubtest suite calls `Run` with a fixture directory and a driver:

```go
func TestFormatFixtures(t *testing.T) {
    ubtest.Run(t, "testdata/ub/format/valid", func(
        name string,
        src []byte,
    ) (string, []string) {
        out, err := Format(src)
        if err != nil {
            return "", []string{err.Error()}
        }
        return string(out), nil
    }, ubtest.Idempotent(), ubtest.Repeat(5))
}
```

The driver receives:

- `name`: fixture path relative to the suite root, without `.ub`.
- `src`: raw fixture bytes.

The driver returns:

- `output`: text compared with `<fixture>.ub.out`.
- `diags`: diagnostics compared with `<fixture>.ub.err`.

A missing `.ub.err` means the fixture must produce no diagnostics. A missing
`.ub.out` means the fixture must produce no output.

## Reading a single fixture

Use these helpers for Go tests that still need direct access to one fixture:

```go
src := ubtest.ReadValidFixture(t, "testdata/ub/check", "root-scope")
raw := ubtest.ReadFixture(t, "testdata/ub/check/invalid/bad-ref.ub")
```

`ReadValidFixture(t, dir, name)` reads `dir/valid/<name>.ub`.

Direct fixture reads are acceptable for pure Go assertions such as exact struct
values, spans, pointer identity, or callback order. If the behavior can be
checked with `Run`, prefer `Run`.

## Options

### `Substring()`

`Substring()` treats each non-empty line in `.ub.err` as a required substring of
the produced diagnostics.

Use it only when the lower layer emits verbose or version-sensitive parser
messages. Prefer exact diagnostics everywhere else.

### `Idempotent()`

`Idempotent()` runs the driver on its own output and checks that the output does
not change. Use it for formatters and renderers.

### `Repeat(n)`

`Repeat(n)` runs every fixture `n` times and compares each result. The default is
2. Use larger counts for formatters or code that could be nondeterministic.

## Updating goldens

Every ubtest suite uses the shared `-update` flag:

```text
go test ./pkg/lang -run TestFormatFixtures -update -count=1
go test ./pkg/lang -run TestFormatFixtures -count=1
```

When `-update` is set:

- `.ub.err` is rewritten from the returned diagnostics.
- `.ub.out` is rewritten from returned output.
- Empty goldens are removed.

Inspect every updated golden before committing.

## Inline source guard

`internal/ubtest` includes repo-level tests that reject inline `.ub` source
strings in Go tests and reject misplaced `.ub` fixtures.

Run the guard after adding or moving fixtures:

```text
go test ./internal/ubtest \
  -run 'TestInlineUB|TestTestFilesAvoidInlineUBSources|TestUBFixturesUseProjectLayout' \
  -count=1
```

The scanner parses Go test files and flags string literals whose first real line
starts with a UB top-level token such as `factory:`, `resources:`, `inputs:`,
`outputs:`, `locals:`, `constraints:`, `project:`, or `state-moves:`.

If a test truly cannot use a fixture, keep the literal small and make the case
specific. Do not add greenlist entries unless there is no fixture-based option.

## Choosing ubtest vs e2etest

Use ubtest when the expected result is one of these:

- parser acceptance or parser diagnostics
- syntax lowering or validation diagnostics
- formatter or canonical output
- type expression parse diagnostics
- import extraction output
- direct Go values that are not command-visible

Use e2etest when the expected result is one of these:

- CLI stdout or stderr
- generated files
- dependency file edits
- compiled factory command behavior
- plan summaries or plan files
- state summaries or state files
- runtime effects, masking, lifecycle, inputs, constraints, or config behavior

When in doubt, choose the user-visible e2e test. Keep package-level ubtest cases
for syntax-only behavior and pure internal invariants.
