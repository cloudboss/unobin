# Benchmark Evidence

This directory holds the collection policy, comparison helpers, and committed performance evidence for Unobin.

Benchmark functions stay in their owning Go packages. Fixed UB workloads stay in package-local `testdata/ub` fixture trees. Raw measurements and comparisons belong under `benchmarks/results/`, outside `docs/`. Temporary caches, binaries, profiles, and incomplete runs belong under a task-owned `_output/benchmarks/` directory.

Every performance change requires a complete before-and-after result set committed in the same change series. A PR description, a document, or an ignored local log does not substitute for those artifacts.

The benchmark tools use a separate Go module to keep their dependencies out of factory builds. `benchstat` is pinned to `golang.org/x/perf v0.0.0-20260929162123-406019bb8b68` in `go.mod` here. `make test` and `make lint` include this module. For host checks, run `go -C benchmarks test ./...` and `go -C benchmarks vet ./...` in addition to the repository's checks.

The evidence validator derives medians, 95% confidence intervals, and nonparametric comparisons from the raw samples. A missing confidence bound is encoded as `null`, with the statistical warning retained. Unit metadata that changes these statistical assumptions requires an explicit policy before use.

## Commands

Each benchmark preparation commit includes a workload JSON file under `benchmarks/workloads/`. It declares the packages, benchmark expression, exact case names and units, fixed workload counts, input paths, correctness commands, setup, cache policy, and primary metric. The collector saves that file's digest as well as the declared inputs. Commands below use a task-owned checkout at the same absolute path for both revisions. Replace the uppercase placeholders with the committed workload file, full preparation commit ID, and a new result name.

```bash
go -C benchmarks run ./cmd/benchmarks collect \
  --repository /path/to/checkout \
  --spec benchmarks/workloads/WORKLOAD.json \
  --fixture-revision FULL_B_ID \
  --out _output/benchmarks/before
go -C benchmarks run ./cmd/benchmarks validate-record \
  --repository /path/to/checkout --record _output/benchmarks/before
```

After checking out the tested implementation commit C, repeat collection with the same workload and preparation ID, using `--out _output/benchmarks/after`. Then produce and validate the finished comparison:

```bash
go -C benchmarks run ./cmd/benchmarks compare \
  --repository /path/to/checkout \
  --before _output/benchmarks/before --after _output/benchmarks/after \
  --out benchmarks/results/WORK_PACKAGE/UTC_TIMESTAMP-DESCRIPTION
go -C benchmarks run ./cmd/benchmarks validate \
  --repository /path/to/checkout \
  --comparison benchmarks/results/WORK_PACKAGE/UTC_TIMESTAMP-DESCRIPTION
```

Run these commands from the tool module with `go -C benchmarks`. When using the compiled CLI elsewhere, provide `--tools /path/to/unobin/benchmarks`. The command checks the comparison executable's embedded version against the module pin. Source and fixture commit IDs must exist in the selected repository.

Collection refuses tracked edits, untracked source or fixture changes, changed fixture content, and existing output directories. It runs correctness checks, a symmetric one-iteration warmup, and the declared samples in order. Failed runs retain raw logs and metadata with their exit status. A source change during collection invalidates the results. Metadata also includes selected non-secret Go settings, cache paths, target, CPU, and dependency-lock digests. Set those controls before collection and keep them identical for both sides.

The comparison copies both raw sample files, test logs, and warmup logs into its new directory. Validation recomputes statistics from those files, checks input digests against both commits, and reruns the pinned tool to verify its output. It does not change the finished result directory.

## Result layout

Create a new directory per work package and comparison. Use a UTC timestamp and short description; keep completed result sets immutable.

```text
benchmarks/
  README.md
  results/
    runtime-indexes/
      20261004T220000Z-composite-children/
        metadata.json
        before.txt
        after.txt
        comparison.txt
        summary.json
```

Use `.txt` for raw Go benchmark output because the repository ignores generic `.out` files. Additional variants, such as full-registry and local-profile builds, get their own named raw files and comparison metadata. Do not commit binaries, Go caches, credentials, local infrastructure state, or machine-wide environment dumps.

## Collection sequence and revisions

1. Commit benchmark code and fixed fixtures independently of the optimization. Call this revision B. Confirm that the benchmark reproduces the identified work and checks the intended result. Declare the primary metric and expected case matrix before measuring. New benchmarks must run on both implementations.
2. Collect `before.txt` at B. Do not change benchmark code, fixtures, workload sizes, or compilation flags after this measurement while treating it as the same comparison. If those change, create a new baseline with matching preparation.
3. Implement the change, run the relevant correctness tests, and collect an initial after measurement to assess it. Resolve failures and unexplained regressions.
4. Commit the tested implementation as revision C. Collect `after.txt` at C using the same fixture code and measurement settings. Include full B and C commit IDs in metadata, along with the separate benchmark fixture revision.
5. Generate the comparison and structured summary, validate the entire result set, and commit the evidence. This subsequent evidence commit avoids a self-reference in which a metadata file would have to contain its own commit ID.

The implementation and evidence commits are one work package. Do not merge or mark a performance package complete without both. Keep terminology changes separate from measured optimizations.

Use a task-owned checkout/worktree when the main workspace has unrelated changes. Keep source and benchmark fixtures clean at the measured revision; pending evidence files are allowed. Prefer the same absolute working path when measuring B and C, and describe any unavoidable differences in metadata. Preserve other workspace files.

## Measurement controls

For Go microbenchmarks:

- Use the same machine, exact Go toolchain, target, benchmark fixtures, and selected dependencies. Start with the repository's Go 1.26.2 baseline; if the toolchain changes, remeasure both revisions using the new toolchain.
- Use `-run '^$'`, `-benchmem`, a fixed benchmark expression, `-p=1`, and fixed CPU settings. Collect at least ten samples per case per side with a target duration of at least two seconds per sample. Benchmark assertions must confirm the graph, output, or feature data being measured.
- Predeclare workload sizes and variants. Include representative small inputs and a scaling series for large or nested inputs. Fix random seeds and traversal data.
- Warm one-time setup symmetrically and state which setup steps are timed. Avoid unrelated CPU-intensive work during collection. If host contention compromises a run, recollect both sides or report the affected pair without claiming an improvement.
- Capture complete stdout/stderr and verify exit status. Avoid pipelines that lose the benchmark command's failure status.

Example for an existing benchmark, run at each measured revision:

```bash
benchmark_results_dir='benchmarks/results/runtime-indexes/20261004T220000Z-plan'
mkdir -p "$benchmark_results_dir"
go test -run '^$' \
  -bench '^BenchmarkPlanLargeAlreadyCheckedFactory$' \
  -benchmem -benchtime=2s -count=10 -cpu=1,4 -p=1 -timeout=30m \
  ./pkg/runtime > "$benchmark_results_dir/before.txt" 2>&1
```

At C, use the identical command with output redirected to `after.txt`. For new benchmarks, metadata includes the exact expression and expected subbenchmark names. For multiple packages, retain `-p=1` to avoid concurrent package runs competing for the same CPU resources.

For compilation benchmarks, additionally separate these cases:

| Case | Preparation | Included measurement |
| --- | --- | --- |
| Warm build | Preinstall toolchain and modules; populate the declared build cache equally | Declared compiler/build stages with an existing cache |
| Cold build | Preinstall toolchain and modules; give each sample a new empty task-owned build cache | Declared stages compiling dependencies with no build-cache entries |
| Generation only | Fixed parsed or source input; report which setup is excluded | Emission time, allocations, and generated bytes |
| Complete compile | Fresh output directory and fixed input/dependency graph | Source analysis, generation, tidy, verification, revision, and Go build |

Use at least five independent samples per side for expensive full builds and state that sampling policy in metadata. Keep downloads outside the measured interval. Use matching linker settings for binary-size comparisons, state whether debug information is retained, and count dependencies using the same command on each side. Ensure retained output files from an earlier run do not change the next workload.

## Metadata and comparison

`metadata.json` must include:

- Evidence format version, benchmark group name, comparison description, and collection timestamps in UTC.
- Full before/after commit IDs, benchmark fixture revision and paths, and selected dependency versions or lock identity. Confirm that measured source was clean.
- Exact commands, relative working directories, benchmark/subbenchmark expressions, workload sizes, sample count, duration, setup policy, and exit status per side.
- Go version, pinned comparison-tool version, OS/architecture, CPU model and logical CPU count, benchmark CPU settings, and relevant target/compiler flags.
- Relevant non-secret environment settings, including `CGO_ENABLED`, `GOFLAGS`, `GOEXPERIMENT`, and the declared cache policy. Report any asymmetric settings.
- Primary metric, additional metrics, units, baseline/after capability sets where they differ, and filenames for all compared raw outputs.

Use the pinned [benchstat tool](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) to produce `comparison.txt` from raw Go benchmark output. Preserve its statistical information and warnings. `summary.json` describes each matched case, units, sample counts, before/after statistics, changes, uncertainty, and the assessment of the declared primary metric. For binary bytes or package counts, include the raw values and reproducible collection commands too.

The evidence validator must reject missing files/sides, unsuccessful runs, zero matched benchmarks, missing expected cases, unit mismatches, changed workload definitions, insufficient samples, or unexplained toolchain/target/cache differences. Relevant tests and their outcomes must accompany the comparison; passing benchmarks alone do not establish correctness.

## Acceptance

An optimization is complete when the committed evidence supports its declared metric and correctness tests pass. Assess variance and statistical confidence before claiming a gain. Report all regressions and capability differences, even when the primary metric improves. A tradeoff needs an explicit rationale and agreement; a neutral or noisy comparison does not substantiate an improvement claim.

For removal of quadratic work, retain the entire workload-size series and evaluate the growth of time and allocations. For editor changes, report cold analysis and warm requests separately. For a new build profile, compare unchanged default capabilities before/after and separately compare the final profiles' capability and size/build-time tradeoff.

Do not enforce machine-specific elapsed-time thresholds in ordinary unit tests. Correctness, expected case coverage, and evidence completeness can be automated; performance comparisons remain tied to the environment used for measurement.
