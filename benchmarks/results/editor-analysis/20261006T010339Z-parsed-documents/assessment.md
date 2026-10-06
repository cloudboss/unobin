# Shared parsed documents

This compares `be6c947efa364e1369fad0dc66f03b7f7efdf957` with
`e9d18576dde03a90d2f6fe7bcf4e1f9439b3f5b1`, using the before revision
as the fixture revision. Workloads, toolchain, caches, local replacements,
and capabilities match. Both correctness suites, warmups, all 180 samples
per side, and source audits pass validation.

Sessions retain parsed syntax and declaration indexes for each document
snapshot and dependency revision. Diagnostics and editor features use
that syntax. Symbols are computed on first use and copied for callers.
Incomplete-source completion keeps its existing repair behavior. Go
schema reads remain fresh; semantic analysis and diagnostics workers are
separate work.

| Case | CPU | Time change | Allocated byte change | Allocation count change |
| --- | ---: | ---: | ---: | ---: |
| Cold open, 50 nodes | 1 | -1.11% | +0.044% | +0.0053% |
| Cold open, 50 nodes | 4 | -1.07% | +0.049% | +0.0061% |
| Cold open, 500 nodes | 1 | +3.14% | +0.277% | +0.020% |
| Cold open, 500 nodes | 4 | +1.31% | +0.274% | +0.020% |
| Cold open, 2,000 nodes | 1 | -4.67% | +0.495% | +0.035% |
| Cold open, 2,000 nodes | 4 | -0.027% | +0.491% | +0.035% |
| 100 requests, 50 nodes | 1 | -48.06% | -35.62% | -35.92% |
| 100 requests, 50 nodes | 4 | -39.24% | -35.73% | -36.07% |
| 100 requests, 500 nodes | 1 | -84.12% | -83.42% | -82.61% |
| 100 requests, 500 nodes | 4 | -82.75% | -83.53% | -82.65% |
| 100 requests, 2,000 nodes | 1 | -95.30% | -95.99% | -94.92% |
| 100 requests, 2,000 nodes | 4 | -95.74% | -96.38% | -94.94% |
| Edit with four features | 1 | -37.91% | -38.42% | -42.44% |
| Edit with four features | 4 | -26.77% | -38.39% | -42.42% |
| Dependency change with four features | 1 | -41.86% | -49.81% | -58.10% |
| Dependency change with four features | 4 | -33.18% | -49.81% | -58.11% |
| Standalone diagnostics control | 1 | +13.80% | +0.137% | +0.134% |
| Standalone diagnostics control | 4 | +23.21% | +0.201% | +0.209% |

All repeated-request, edit, and dependency-change improvements are
significant. The 2,000-node request batch decreases from 30.080 to
1.413 seconds at CPU 1 and from 20.875 to 0.889 seconds at CPU 4.
The batch includes the first symbol calculation after initial diagnostics.

Cold-open time changes are inconclusive at 50 nodes and CPU 1, and at
2,000 nodes for both CPU settings. The 50-node CPU 4 improvement and
both 500-node regressions are significant. At 500 nodes, the CPU 1
median increases from 210.407 to 217.013 milliseconds. All cold-open
byte and allocation increases are significant. The indexes add about
30 KB, 294 KB, and 1.17 MB per cold operation at these document sizes.

The standalone control does not use the session cache. Its parsed
validation now delegates to a private helper so sessions can share that
code. Its time regression is significant for both CPU settings: 255.625
to 290.902 milliseconds at CPU 1 and 193.043 to 237.849 milliseconds
at CPU 4. Its bytes and allocation counts also increase significantly.
This comparison does not isolate the cause of the timing regression.
It does not support a speed improvement for standalone diagnostics.

The whole workload has a mixed primary assessment. The saved samples,
confidence intervals, medians, and p-values are in `summary.json` and
the raw comparison files. Allocation totals measure work per operation;
retained heap per open document was not measured.
