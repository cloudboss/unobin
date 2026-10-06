# Typed Go emission

This compares `6807203ebb708b87ce22e3d4f27a9d585fb0603d` with `140f2147f47f6b5d1b7b9089bce04693c4026a6e`, using the before revision as the fixture revision. Both sides use the same workloads, toolchain, cache settings, and default capabilities. Correctness tests, warmups, all 80 samples per side, and source audits pass validation.

The generator accepts typed composites and tracks imports during syntax emission. The public generation adapters remain available. Empty composites and addresses containing `lang` no longer produce unused imports. Root entry points have one library setup and one runner call. Compiled consumer tests cover these cases and source spans, assets, configuration, defaults, constraints, and composite iteration.

| Case | CPU | Time change | Allocated byte change | Allocation count change |
| --- | ---: | ---: | ---: | ---: |
| 1 composite | 1 | +1.13% | +0.25% | +0.039% |
| 1 composite | 4 | +1.67% | +0.25% | +0.039% |
| 10 composites | 1 | +3.79% | +0.31% | +0.036% |
| 10 composites | 4 | +1.60% | +0.30% | +0.036% |
| 100 composites | 1 | +2.51% | +0.32% | +0.035% |
| 100 composites | 4 | +1.68% | +0.32% | +0.035% |
| 1,000-action factory | 1 | +0.36% | -0.040% | +0.0044% |
| 1,000-action factory | 4 | +0.59% | -0.067% | +0.0041% |

All composite time differences are significant. The 10-composite CPU 1 median increases from 10.365 to 10.758 milliseconds; the 100-composite CPU 1 median increases from 102.497 to 105.068 milliseconds. Factory time differences are inconclusive (p=0.1903 and p=0.0892).

All byte and allocation-count differences are significant. Composite allocation medians increase by 2, 11, and approximately 101 allocations per operation. The public adapter constructs typed composite inputs; the measurements include that conversion. Already ordered inputs are used directly, and adapter slices use known capacities. The comparison does not isolate the timing cost of individual implementation changes.

Composite output sizes remain 10,361, 60,077, and 557,237 bytes. Generated library calls remain 2, 11, and 101. Factory output increases by 27 bytes, from 495,482 to 495,509, for the common library setup. These measurements do not establish that Go literal size requires a different representation; complete build comparisons measure that cost separately.

The workload's primary assessment is regressed. The correctness fixes and typed generation boundary remain useful, but this comparison does not support a speed or allocation-count improvement. Exact samples, medians, confidence intervals, and p-values are in `summary.json` and the raw comparison files.
