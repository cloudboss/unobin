# Shared editor analysis

This compares `be6c947efa364e1369fad0dc66f03b7f7efdf957` with `bd4d70c12fdc3ad479da58d6e26d70486154ebea`, using the baseline as the fixture revision. Workloads, toolchain, cache settings, and capabilities match. Correctness checks, warmups, all 180 samples per side, and source and fixture audits pass validation.

Editor sessions reuse parsed syntax, declaration indexes, resolved imports, Go schemas, and checked types by document snapshot and dependency revision. Diagnostics and features consume the same checked body. Edits and watched-file changes invalidate the analysis. Tests verify exact symbols, definition spans, completion labels, hover types, and diagnostic versions.

| Case | CPU | Time change | Allocated byte change | Allocation count change |
| --- | ---: | ---: | ---: | ---: |
| Cold open, 50 nodes | 1 | +5.72% | +0.142% | +0.067% |
| Cold open, 50 nodes | 4 | +3.66% | +0.147% | +0.067% |
| Cold open, 500 nodes | 1 | +9.52% | +0.338% | +0.060% |
| Cold open, 500 nodes | 4 | +3.89% | +0.339% | +0.060% |
| Cold open, 2,000 nodes | 1 | +0.55% | +0.524% | +0.053% |
| Cold open, 2,000 nodes | 4 | +2.23% | +0.519% | +0.052% |
| 100 requests, 50 nodes | 1 | -99.90% | -99.52% | -99.987% |
| 100 requests, 50 nodes | 4 | -99.91% | -99.52% | -99.987% |
| 100 requests, 500 nodes | 1 | -99.68% | -98.92% | -99.996% |
| 100 requests, 500 nodes | 4 | -99.60% | -98.92% | -99.996% |
| 100 requests, 2,000 nodes | 1 | -99.57% | -99.20% | -99.997% |
| 100 requests, 2,000 nodes | 4 | -99.59% | -99.26% | -99.998% |
| Edit with four features | 1 | -50.01% | -47.16% | -51.81% |
| Edit with four features | 4 | -47.19% | -47.03% | -51.69% |
| Dependency change with four features | 1 | +3.97% | -3.28% | -5.66% |
| Dependency change with four features | 4 | -5.71% | -3.25% | -5.66% |
| Standalone diagnostics control | 1 | +9.78% | -0.076% | -0.094% |
| Standalone diagnostics control | 4 | -3.90% | -0.127% | -0.143% |

All repeated-request and edit improvements are significant at alpha=0.05. The 2,000-node batch of 100 requests falls from 30.080 seconds to 130.30 milliseconds at CPU 1 and from 20.875 seconds to 86.30 milliseconds at CPU 4. The four-feature edit operation falls from 573.17 to 286.55 milliseconds at CPU 1 and from 361.15 to 190.73 milliseconds at CPU 4.

Cold operations include cache construction. All cold allocation increases are significant: about 95-99 KB, 361-363 KB, and 1.24 MB per operation at the three document sizes. Cold-open time differences are significant except at 2,000 nodes and CPU 1. The 500-node CPU 1 median rises from 210.41 to 230.44 milliseconds. The standalone control reparses and checks each operation; its time changes are significant, its CPU 4 allocation reductions are significant, and its CPU 1 allocation changes are inconclusive. The comparison does not isolate the cause of the cold timing regressions.

Dependency invalidation allocates significantly fewer bytes and objects at both CPU settings. Its CPU 1 time change is inconclusive (p=0.16549); its CPU 4 time reduction is significant. The workload's primary assessment is mixed: repeated features and edits improve while cold opens incur extra work.

These benchmarks call the session API directly. Queued edits through the public server and diagnostic scheduling have separate measurements under `queued-editor-analysis`. This pair includes all implementation changes between the source revisions. Allocated bytes measure cumulative work per operation; retained heap and process memory were not measured. Complete samples, medians, confidence intervals, p-values, correctness logs, and metadata are included beside this assessment.
