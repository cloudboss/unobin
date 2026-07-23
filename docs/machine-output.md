# Machine output

Unobin commands use versioned JSON or Unobin output when `--format json` or
`--format unobin` is selected. Most commands write one complete document. Apply
streams one complete record per line as work progresses; in JSON, this is JSON
Lines.

Each actual JSON document or stream record is compact and followed by one newline.
Examples in this document may be indented for readability. Actual Unobin output
is also one compact literal per line.

```json
{"kind":"version","format-version":1,"name":"unobin","version":"v0.10.0","diagnostics":[]}
```

## Selecting an output format

Standard commands accept `--format text|json|unobin`; `text` is the default.
Developer and compiled `print-graph` commands additionally accept `dot`. DOT is a
payload format, not a versioned machine contract, and its application errors are
text on stderr.

An unsupported format is reported as text on stderr, for example:

```text
--format: unknown 'yaml' (want text, json, unobin)
```

Graph errors also list `dot`.

The following payload and protocol commands use command-specific output:

- `unobin fmt` writes formatted source, changed paths, or files.
- `factory schema template` writes a `.ub` stack skeleton or a requested file.
- `factory state pull` writes a raw decrypted JSON snapshot that may contain
  sensitive values.
- `unobin lsp` owns stdio for the language-server protocol.
- Help remains text, and shell completion retains its protocol output.

For those commands, `--format` produces a plain-text unknown-flag error.

## When machine output takes effect

Arguments and flags are parsed and validated before the format selected by
`--format` takes effect. Help, usage, unknown commands, unknown or malformed
flags, missing flag values, missing required flags, and positional-argument
errors are therefore text. Help goes to stdout. Invocation errors go to stderr.
Shell completion retains its protocol output.

After invocation validation succeeds, source, state, provider, backend, resolver,
dependency, and external-tool failures use the selected machine contract.

| Situation | stdout | stderr | Exit |
| --- | --- | --- | --- |
| Help or successful shell completion | Text or completion protocol | Empty | 0 |
| Invocation error | Empty unless the invocation is a completion request | Bare text | 1 |
| Successful text command | Result or payload | Warnings, notices, and progress | 0 |
| Failed text command | Any command-specific partial output | Bare error and diagnostics | 1 |
| Successful single-document machine command | One result document | Empty | 0 |
| Negative machine result | One result with `ok: false` | Empty | 1 |
| Machine operation failure | One `command-error` document | Empty | 1 |
| Machine apply | A versioned JSON Lines or Unobin record stream | Empty | 0 or 1 |
| Response encoding or stdout write failure | May be malformed or truncated | Bare response-channel error if the process survives | 1 or signal status |

Plain-text invocation errors contain the error message followed by one newline.

After invocation validation succeeds, machine output uses stdout. Response
encoding or stdout write failures may report a bare response-channel error on
stderr if the process survives. A closed stdout can also terminate the process
through SIGPIPE before stderr is written.

## Encoding and values

Every document and record starts with `kind` and `format-version`. All contracts
in this reference currently use format version `1`. Single-document results have
no timestamps.

JSON documents are UTF-8, compact JSON with HTML escaping disabled and one
trailing newline. They are encoded completely before the first write. Apply JSON
is JSON Lines: one complete compact object per line, no blank lines, and a
trailing newline after every record.

Unobin documents and stream records use the same logical fields as JSON. Each is
one compact literal followed by one newline.

Machine values may contain:

- null, booleans, strings, and finite numbers;
- arrays and slices of permitted values;
- maps with string keys and permitted values;
- response objects composed recursively from those values;
- RFC 3339 Nano timestamps.

Cycles, non-finite numbers, non-string map keys, duplicate keys after invalid
UTF-8 replacement, functions, channels, complex numbers, `json.Number`, raw
durations in dynamic values, and format-specific custom marshalers are rejected.
Invalid UTF-8 is replaced with the Unicode replacement character in both formats.

Required collection fields are always arrays or objects, never null. This applies
to `diagnostics`, `files`, `dependencies`, `inputs`, `outputs`, `nodes`, `edges`,
`state-moves`, `steps`, `replace-triggers`, `depends-on`, `sensitive`,
`sensitive-inputs`, `sensitive-outputs`, `entries`, `snapshots`, and `mismatches`.

In the contract tables below, every field is required unless it is explicitly
marked "omitted when absent." A field whose type includes null is still required
and is encoded as null when absent.

### Deterministic ordering

Unobin sorts string map keys, as does Go's JSON encoder. Other collections use
these rules:

- dependencies and verification mismatches by id;
- composed file changes by path, then action;
- diagnostics by path, position, severity, code, message, and hint;
- graph nodes by address and edges by `from`, then `to`;
- schema inputs and outputs in declaration order;
- plan steps by address and state moves in semantic execution order;
- state entries by address and snapshots in backend chronological order;
- sensitivity names, `depends-on`, and replace triggers lexically;
- apply records by `sequence`, which is authoritative over timestamps.

## Compatibility

Each `kind` has an independent format-version sequence. Consumers must ignore
unknown fields. Apply consumers must also ignore unknown nonterminal record kinds,
but must recognize the last record as `apply-result` or `apply-error`. An unknown
terminal kind is an unsupported stream contract.

Adding an optional field does not increment a version. Adding a nonterminal apply
kind starts that kind at version 1 without changing other kinds. Removing or
renaming a field, changing its type, nullability, semantics, or required ordering,
making an optional field required, or changing an enum meaning increments the
containing kind. Adding an enum value also increments the containing kind unless
that field explicitly accepts unknown values. An incompatible shared nested-value
change increments every kind that contains it. Changing terminal apply kinds or
the terminal-last guarantee increments both terminal kinds.

## Paths and sensitive values

Dedicated machine path fields always use forward slashes.

1. A user-supplied relative path retains its lexically cleaned relative spelling.
2. A user-supplied absolute path remains absolute.
3. A file below a user-supplied directory uses that display path plus its suffix.
4. Discovered project files are relative to the project.
5. Dependency source uses logical dependency display paths.
6. Cache roots, temporary directories, module-cache paths, and other discovered
   local absolute paths are not exposed in dedicated fields or known internal
   messages.
7. An unmappable absolute path is reduced to its base name in a diagnostic path.

File-change paths are relative to the command working directory unless the
corresponding destination argument was absolute.

These guarantees apply to dedicated fields and messages produced by Unobin.
Opaque provider, resolver, operating-system, and external-tool messages remain
verbatim except for known workspace, project, cache, and temporary prefixes. They
may contain paths or sensitive text.

Asset path and content values remain logical in plans and state. Machine values
never replace them with a source path, cache path, or embedded content. Consumers
must treat their encoded strings as opaque references. Text plans render them as
source-like values such as `<asset.lambda.path>` and
`<asset.lambda['main.go'].content>`.

Known sensitive values are replaced with exactly `<sensitive>` in apply outputs,
output results, state entry inputs and outputs, and text plans. Plan summaries do
not include values or sensitivity lists. Unobin does not claim to redact arbitrary
provider or external-tool error strings; those diagnostic messages remain
verbatim.

## Shared nested values

### Diagnostic

| Field | Type | Meaning |
| --- | --- | --- |
| `code` | string | Stable code where one is defined; diagnostic codes are otherwise extensible. |
| `severity` | enum | `error`, `warning`, or `info`. |
| `message` | string | Prefix-free diagnostic message. |
| `hint` | string | Omitted when absent. |
| `path` | string | Omitted when absent. |
| `span` | span | Omitted when absent. |

A span has required `start` and optional, exclusive `end` positions. Each position
has required integer `line`, `column`, and `offset` fields. Line and column are
one-based; column counts bytes. Offset is zero-based bytes from the input start.

Source error codes initially map to `unobin.parse`, `unobin.lex`,
`unobin.schema`, `unobin.type`, `unobin.resolve`, and the fallback
`unobin.error`.

### File change

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | Public path of the attempted mutation. |
| `action` | enum | `created`, `updated`, `removed`, or `unchanged`. |

Actions compare the path before its first command mutation with its final observed
state, including partial failure. Repeated mutations are composed to at most one
entry per path. An absent path created and then removed has no final effect and is
omitted.

### Factory identity

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Factory name. |
| `version` | string | Factory version. |
| `content-revision` | string, except nullable in `compile-result` | Compiled content identity. |
| `library-path` | string or null | Public factory library path, if configured. |

### Binding

| Field | Type | Meaning |
| --- | --- | --- |
| `library-path` | string or null | Public library path. |
| `alias` | string | Source import alias. |
| `export` | string | Selected library export. |

### Target

A target has required string fields `path` and `type`. Target type is one of
`factory`, `library`, `stack`, `project`, `project-lock`, or `directory`.

## Single-document contracts

Every successful single-document result has a required `diagnostics` array. A
negative result such as `ok: false` is still a normal result document, not a
`command-error`.

### Command error

Any single-document command can emit kind `command-error` version 1.

| Field | Type | Meaning |
| --- | --- | --- |
| `command` | string | Command path without the executable name. |
| `code` | enum | Failure class. |
| `message` | string | Stable command-level summary. |
| `diagnostics` | diagnostic array | Every collected and converted cause. |
| `files` | file-change array | Known effects before the failure. |

Command error codes are `unobin.command.invalid-args`, `unobin.command.io`,
`unobin.command.failed`, and `unobin.command.stdout-conflict`.

### Developer command results

#### `version`

Produced by `unobin version`. Fields are `name` (`unobin`), `version`, and
`diagnostics`.

#### `check-result`

Produced by `unobin check`. Fields are `ok`, `target`, and `diagnostics`. Error
diagnostics make `ok` false and exit 1; warnings do not. Failure before the target
can be identified uses `command-error`.

#### `compile-result`

Produced by `unobin compile`.

| Field | Type | Meaning |
| --- | --- | --- |
| `factory` | compile factory identity | `content-revision` is null without `--build`. |
| `source` | object | Required `path` and `project-dir` strings. |
| `output` | object | Required `dir`, `main-go`, `go-mod`, `built`, and `binary`; optional `assets`; `binary` is string or null. |
| `files` | file-change array | Composed effects for generated Go and UB files, `go.sum`, and the binary. |
| `diagnostics` | diagnostic array | Includes captured Go tool output in machine mode. |

`binary` and `content-revision` are non-null only after a build. Machine compile
rejects `-o -` with `unobin.command.stdout-conflict`. External-tool stdout and
stderr are each bounded to 1 MiB and reported as diagnostics rather than written
outside the document.

`output.assets` is the public path to `factory.assets` and is omitted when the
compiled source has no captured assets. Changes to the sidecar also appear in
`files`, including its removal when a later compile has no assets.

#### Dependency results

| Kind | Command | Required fields after the common header |
| --- | --- | --- |
| `dependency-list` | `unobin deps list` | `dependencies`, `diagnostics` |
| `dependency-sync-result` | `unobin deps sync` | `project-file`, `lock-file`, `direct`, `indirect`, `selected`, `files`, `diagnostics` |
| `dependency-get-result` | `unobin deps get` | `dependency`, `version`, `indirect`, `project-file`, `lock-file`, `direct`, `selected`, `files`, `diagnostics` |
| `dependency-verify-result` | `unobin deps verify` | `ok`, `checked`, `mismatches`, `diagnostics` |
| `dependency-cache-clean-result` | `unobin deps clean` | `removed`, `diagnostics` |

Each dependency entry has required `id`, `kind`, `version`, and `indirect` fields.
Dependency kind is `ub` or `go`. A verification mismatch has required `id`,
`expected-hash`, `actual-hash`, and `message` fields. Mismatches produce
`ok: false` and exit 1; fetch or I/O failure uses `command-error`. `removed` is
true only when the dependency cache existed before cleaning.

#### Generator results

| Kind | Command | Required fields after the common header |
| --- | --- | --- |
| `factory-generation-result` | `unobin generate factory` | `output-dir`, `files`, `diagnostics` |
| `ub-library-generation-result` | `unobin generate ublibrary` | `output-dir`, `type`, `files`, `diagnostics` |
| `go-library-generation-result` | `unobin generate golibrary` | `output-dir`, `module-path`, `provider`, `resources`, `data-sources`, `files`, `diagnostics` |

`provider` preserves the exact requested provider, including an organization
prefix. Partial generation failures attach known effects to `command-error.files`.

### Shared graph result

Developer and compiled `print-graph` produce kind `graph`. Fields are `name`,
`nodes`, `edges`, and `diagnostics`.

Each node has required `address`, `category`, `binding`, and `composite` fields.
`binding` is a binding object or null. Category is `resource`, `data-source`,
`action`, `output`, or `library-config`. Each edge has required `from` and `to`
strings. An edge may target an `input.*` root that is absent from `nodes`.

### Compiled factory results

#### Identity, validation, and schema

| Kind | Command | Required fields after the common header |
| --- | --- | --- |
| `factory-version` | `factory version` | `factory`, `diagnostics` |
| `validation-result` | `factory validate` | `ok`, `target`, `diagnostics` |
| `schema` | `factory schema show` | `factory`, `inputs`, `outputs`, `diagnostics` |

Validation error diagnostics make `ok` false and exit 1; warnings do not. Each
schema input has required `name`, `type`, `default`, `description`, and
`sensitive` fields. `default` is canonical Unobin expression text or null. Each
schema output has required `name`, `description`, and `sensitive` fields. Type is
canonical Unobin type-expression text. Missing descriptions are empty strings.

Bare `factory schema` prints help and exits 0. `schema template` remains the
payload command described above.

#### `plan-summary`

Produced by `factory plan`.

| Field | Type | Meaning |
| --- | --- | --- |
| `factory` | factory identity | Compiled factory identity. |
| `stack` | string | Stack name. |
| `plan-digest` | string or null | `sha256:` digest of a written sealed artifact. |
| `file` | file change or null | Plan artifact effect. |
| `state-rev` | string or null | State revision used by the plan. |
| `parallelism` | integer | Effective apply parallelism. |
| `destroy` | boolean | Whether this is a destroy plan. |
| `summary` | object | Required count for every decision. |
| `state-moves` | array | Required `{from, to}` objects in execution order. |
| `steps` | array | Public plan step summaries. |
| `diagnostics` | diagnostic array | Collected notices and warnings. |

The summary has required integer fields `create`, `read`, `update`, `replace`,
`destroy`, `rerun`, `skip`, `no-op`, and `eval`. These are also all decision enum
values.

Each step has required `address`, `category`, `decision`, `composite`, `drift`,
`gone`, `replace-triggers`, and `deferred-config`. `deferred-config` is string or
null. Step category uses the graph category enum. A summary contains no input,
output, prior, or observed values and no sensitivity lists. Without `-o`, both
`plan-digest` and `file` are null.

#### Refresh and output

| Kind | Command | Required fields after the common header |
| --- | --- | --- |
| `refresh-result` | `factory refresh` | `factory`, `stack`, `ok`, `refreshed`, `removed`, `state-rev`, `diagnostics` |
| `outputs` | `factory output` | `factory`, `stack`, `outputs`, `sensitive`, `diagnostics` |
| `output` | `factory output NAME` | `factory`, `stack`, `name`, `value`, `sensitive`, `diagnostics` |

`refresh-result.state-rev` is string or null. If refresh wrote state and then
failed, it reports `ok: false`, completed counts, the latest revision, error
diagnostics, and exits 1. Before any observed write, failure uses `command-error`.
Sensitive output values are masked and their names are listed lexically.

#### Pin and state inspection

| Kind | Command | Required fields after the common header |
| --- | --- | --- |
| `pin-result` | `factory pin` | `stack`, `action`, `file`, `factory`, `diagnostics` |
| `state-list` | `factory state list` | `factory`, `stack`, `state-rev`, `entries`, `diagnostics` |
| `state-entry` | `factory state show` | `factory`, `stack`, `state-rev`, `entry`, `diagnostics` |
| `state-snapshots` | `factory state snapshots list` | `factory`, `stack`, `current`, `snapshots`, `diagnostics` |

Pin action is `added-factory-block`, `added-pin-block`,
`added-supported-versions`, `appended-entry`, or `already-pinned`. The last action
uses an `unchanged` file change.

`state-list.state-rev` and `state-snapshots.current` are string or null. A state
entry summary has required `address`, `entry-type`, `category`, and `binding`.
Entry type is `leaf`, `library-call`, `action`, or `data-source`. State category is
`resource`, `data-source`, or `action`. Binding is required for valid current
entries.

A detailed `state-entry.entry` adds required `schema-version`, `trigger-hash`,
`inputs`, `outputs`, `depends-on`, `sensitive-inputs`, and `sensitive-outputs`.
`trigger-hash` is string or null. Each snapshot has required `revision` and
`current` fields. Snapshots remain in backend chronological order.

#### State mutation

| Kind | Command | Required fields after the common header |
| --- | --- | --- |
| `state-move-result` | `factory state move` | `factory`, `stack`, `ok`, `from`, `to`, `moved`, `state-rev`, `diagnostics` |
| `state-remove-result` | `factory state remove` | `factory`, `stack`, `ok`, `address`, `state-rev`, `diagnostics` |
| `state-gc-result` | `factory state snapshots gc` | `factory`, `stack`, `ok`, `deleted`, `kept`, `current`, `failed-revision`, `diagnostics` |
| `state-force-unlock-result` | `factory state force-unlock` | `factory`, `stack`, `unlocked`, `diagnostics` |

`state-gc-result.current` and `failed-revision` are string or null. A mutation that
writes a new current snapshot and then fails, including during unlock, reports its
normal result kind with `ok: false`, completed effects, the latest revision, error
diagnostics, and exit 1. A failure before a new revision is observed uses
`command-error`. GC similarly retains completed deletion counts and the failed
revision.

## Apply stream

`factory apply --format json` emits JSON Lines. Unobin apply emits the equivalent
one-literal-per-line stream. Every record has required `kind`, `format-version`,
`sequence`, and `timestamp` fields. Sequence starts at 1 and increments after each
complete record write. Timestamp is RFC 3339 Nano UTC and is sampled immediately
before encoding; sequence defines order.

The stream order is:

```text
command-diagnostic*
apply-ui?
(apply-event | command-diagnostic)*
apply-output*
apply-result | apply-error
```

Startup diagnostics precede the optional UI record. First-interrupt and
browser-open diagnostics are asynchronous and may follow runtime events. Their
sequence records their actual order. No diagnostic or event follows the first
output. Failure emits no outputs. Runtime failure events go to the UI but are not
encoded as `apply-event`; the terminal `apply-error` represents them.

### `command-diagnostic`

Adds one required `diagnostic` object to the common stream fields.

### `apply-ui`

Adds one required `url` string. It appears at most once. `--ui` is valid in every
format. Browser-open failure or the five-second timeout emits a diagnostic with
code `unobin.ui.browser-open` and apply continues; UI server startup failure is a
setup-stage `apply-error`.

### `apply-event`

Adds required `stage`, `decision`, and `address`. Stage is `start` or `done`.
`elapsed` is omitted for `start` and required for `done`. Decision uses the plan
decision enum. Composite boundaries, outputs, no-op resources, and skipped actions
retain the command's silent-event filtering.

### `apply-output`

Adds required `name`, `value`, and `sensitive`. Records sort by name. All effective
values are validated before the first output is written. A sensitive value is
replaced before validation, so its original value is never inspected by the
machine encoder. An apply with no outputs emits no output records.

### `apply-result`

This success terminal adds required `started-at`, `finished-at`, `elapsed`,
`state-rev`, and `output-count`. The timestamps are RFC 3339 Nano UTC; elapsed uses
Unobin's short duration rendering. `output-count` equals the number of preceding
apply-output records.

### `apply-error`

This failure terminal has the following fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `started-at` | timestamp | Apply stream start. |
| `finished-at` | timestamp | Terminal-record time. |
| `elapsed` | duration string | Total elapsed time. |
| `stage` | enum | `setup`, `execute`, or `finalize`. |
| `code` | enum | Stable apply failure code. |
| `message` | string | Stable operation summary. |
| `state-rev` | string or null | Latest observable current revision. |
| `diagnostics` | diagnostic array | Converted underlying causes. |
| `address` | string | Omitted when unavailable. |
| `decision` | decision enum | Omitted when unavailable. |
| `library` | string | Public library path; omitted when unavailable. |
| `skipped` | integer | Required, including zero, when a runtime step error supplies counts; otherwise omitted. |
| `succeeded` | integer | Required, including zero, when a runtime step error supplies counts; otherwise omitted. |

Apply error codes are `unobin.apply.setup-failed`,
`unobin.apply.step-failed`, `unobin.apply.finalize-failed`, and
`unobin.apply.interrupted`. Setup failures omit step fields. Execution step
failures include available step fields. Finalization includes output evaluation,
final state persistence, and lock release.

### Interrupts and terminal records

The first SIGINT requests a scheduler drain and emits
`unobin.apply.drain-requested` with this prefix-free diagnostic message:

```text
Interrupted; letting in-flight steps finish. Press Ctrl-C again or send SIGTERM to abort.
```

After 60 seconds, a second SIGINT, or any SIGTERM, cancellation produces terminal
code `unobin.apply.interrupted`. A handled interrupt exits 1 after that terminal
record. Abrupt process termination uses the platform signal status and may not
write a terminal record.

When response encoding and stdout writes succeed, an apply stream contains
exactly one recognized `apply-result` or `apply-error`, and it is the last record.
An encoding failure may be replaced by a primitive finalization `apply-error` if
stdout remains usable. A stdout write failure cannot safely emit another record.

Consumers must treat malformed output, a partial line, or a stream without a
recognized terminal record as a transport failure. A transport failure does not
prove that apply had no effects. Inspect current state and compute a new plan
before applying again; do not blindly retry the same plan.

## Exit status

- 0: successful commands and help, including results with warnings.
- 1: invocation errors, text application failures, `command-error`, negative
  results such as `ok: false`, handled interrupts, and `apply-error`.
- Platform signal status: abrupt termination, including default SIGPIPE behavior.

There are no additional numeric status classes. Machine consumers classify
nonzero results by `kind`, `code`, and result fields.
