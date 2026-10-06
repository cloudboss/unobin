# Shared import graph measurements

The primary time comparison improves at both CPU settings. Nested and shared library analysis is 42.43–49.96% faster and allocates 44.77–48.10% fewer bytes. Preflight and semantic analysis use the same resolved imports and parsed UB libraries. Source inventories are checked again before analysis returns.

| Workload | CPUs | Before | After | Time change |
| --- | ---: | ---: | ---: | ---: |
| Ten nested libraries | 1 | 10.103 ms | 5.148 ms | -49.04% |
| Ten nested libraries | 4 | 8.238 ms | 4.256 ms | -48.33% |
| One hundred shared exports | 1 | 169.942 ms | 85.467 ms | -49.71% |
| One hundred shared exports | 4 | 134.683 ms | 71.077 ms | -47.23% |

The shared-export workload reads UB source 107 times per operation, down from
208. It still resolves two sources and derives one Go schema per operation. Ten nested libraries use 44 UB reads, down from 64. Semantic assertions verify every export and the schema reached through each nested library.

Standalone schema-cache controls use unchanged implementation code. Their measured time reductions are not attributed to import graph reuse. The unchanged cache control at four CPUs reports 67,344.5 allocations per operation, compared with 67,344 before: +0.00074%, with p=0.0186. All other allocation increases in these controls are statistically inconclusive. Raw samples and confidence bounds are retained in the comparison files.

Each side contains ten two-second samples at one and four CPUs, after the same correctness commands and one-iteration warmup. The Go schema reader is injected in the import-analysis workloads; these measurements include resolution and metadata checks but exclude real schema derivation. Both collections used the same checkout path, Go 1.26.2, target, caches, and committed fixtures. Metadata identifies both source revisions and all input digests. The validator recomputes the statistics and checks the pinned comparison output.
