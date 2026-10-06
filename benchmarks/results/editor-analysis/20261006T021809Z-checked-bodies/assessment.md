# Shared document syntax, imports, and types

This compares `be6c947efa364e1369fad0dc66f03b7f7efdf957` with `990d0ad54972e854a5046fc499836cc1e82aa708`, using the baseline as the fixture revision. The paired workload, toolchain, cache settings, and capabilities match. Correctness checks, warmups, all 180 samples per side, and source and fixture audits pass validation.

Sessions retain syntax, declaration indexes, resolved imports, Go schemas, and inferred local types by document snapshot and dependency revision. Diagnostics and features share the checked body, including useful types when other expressions have errors. Watched-file changes invalidate the result. Incomplete-source completion retains its repair behavior.

| Case | CPU | Time change | Allocated byte change | Allocation count change |
| --- | ---: | ---: | ---: | ---: |
| Cold open, 50 nodes | 1 | +0.38% | +0.243% | +0.190% |
| Cold open, 50 nodes | 4 | -1.27% | +0.245% | +0.191% |
| Cold open, 500 nodes | 1 | +3.34% | +0.403% | +0.138% |
| Cold open, 500 nodes | 4 | -0.99% | +0.403% | +0.138% |
| Cold open, 2,000 nodes | 1 | -6.31% | +0.552% | +0.088% |
| Cold open, 2,000 nodes | 4 | -2.29% | +0.548% | +0.087% |
| 100 requests, 50 nodes | 1 | -99.90% | -99.52% | -99.987% |
| 100 requests, 50 nodes | 4 | -99.91% | -99.52% | -99.987% |
| 100 requests, 500 nodes | 1 | -99.76% | -98.92% | -99.996% |
| 100 requests, 500 nodes | 4 | -99.69% | -98.92% | -99.996% |
| 100 requests, 2,000 nodes | 1 | -99.59% | -99.22% | -99.997% |
| 100 requests, 2,000 nodes | 4 | -99.63% | -99.26% | -99.998% |
| Edit with four features | 1 | -54.69% | -47.13% | -51.77% |
| Edit with four features | 4 | -52.34% | -47.00% | -51.65% |
| Dependency change with four features | 1 | -6.34% | -3.23% | -5.59% |
| Dependency change with four features | 4 | -10.83% | -3.20% | -5.59% |
| Standalone diagnostics control | 1 | +4.22% | +0.0054% | +0.0023% |
| Standalone diagnostics control | 4 | -4.34% | +0.000069% | +0.0022% |

All repeated-request, edit, and dependency-change improvements are significant at the declared 0.05 threshold. The 2,000-node request batch decreases from 30.080 seconds to 122.50 milliseconds at CPU 1, and from 20.875 seconds to 77.88 milliseconds at CPU 4. Requests include symbols, definition, hover, and completion, with fixed positions and checked results.

Cold-open time changes are inconclusive at 50 nodes and CPU 1, and at 500 nodes and CPU 4. Other cold-open time differences are significant. The 500-node CPU 1 median increases from 210.41 to 217.42 milliseconds. All cold-open allocation increases are significant: about 164 KB, 430 KB, and 1.31 MB per operation at the three document sizes.

The standalone control reparses and checks each operation. Its time differences are significant: CPU 1 increases from 255.63 to 266.40 milliseconds; CPU 4 decreases from 193.04 to 184.67 milliseconds. Its CPU 1 allocation increases are significant; CPU 4 allocation changes are inconclusive. This comparison does not isolate the cause of the remaining timing regressions. The whole workload has a mixed primary assessment.

These session benchmarks call the session API directly. Pending edits through the public server and diagnostic worker scheduling are measured separately under `queued-editor-analysis`. This comparison includes all editor changes between the revisions and does not isolate their individual effects. Allocated bytes measure cumulative work per operation; retained heap per open document and process memory were not measured. Raw samples, metadata, confidence intervals, medians, and p-values are included beside this assessment.
