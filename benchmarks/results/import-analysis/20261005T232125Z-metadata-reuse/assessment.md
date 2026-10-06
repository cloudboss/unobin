# Go metadata reuse during import analysis

Shared-export analysis is 4.35-10.17% faster than the resolved-graph implementation and allocates 5.42-10.01% fewer bytes. One analysis reuses package checks, source snapshots, and configuration metadata across exports. Source revisions are checked before the result is returned; subsequent requests use current source checks.

| Workload | CPUs | Before | After | Time change |
| --- | ---: | ---: | ---: | ---: |
| Ten nested libraries | 1 | 5.148 ms | 4.967 ms | -3.53% |
| Ten nested libraries | 4 | 4.256 ms | 4.189 ms | -1.59%, inconclusive |
| One hundred shared exports | 1 | 85.467 ms | 77.374 ms | -9.47% |
| One hundred shared exports | 4 | 71.077 ms | 63.849 ms | -10.17% |

The shared-export cases retain two resolutions, one schema read, and 107 UB source reads per operation. The reduction comes from Go metadata work within those reads. The existing nested-library generation workload is 5.12-5.92% faster and allocates 4.55-4.63% fewer bytes.

The factory with no imports allocates five additional objects and 371-377 extra bytes per operation for the independent analysis context. Its time improves, but it does not exercise metadata reuse. Standalone schema-cache controls retain current-source checks. Their unchanged-source time differences are inconclusive. The helper-edit control at one CPU allocates 16 extra bytes, or 0.00673%; its allocation count is unchanged. Raw samples retain all control results.

Each side contains ten two-second samples at one and four CPUs, after the same correctness commands and one-iteration warmup. The injected Go schema reader excludes real schema derivation from the import-analysis workloads. Both sides use Go 1.26.2, the same checkout path, target, caches, and committed fixtures. Metadata identifies source revisions and input digests. Validation recomputes the statistics, audits the source inputs, and checks the pinned comparison output.
