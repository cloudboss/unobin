# Factory generation with Go metadata reuse

These factories import only UB libraries, so they do not benefit from Go metadata reuse. At four CPUs, the small case is 1.47% slower and the large case is 0.79% slower. Both time differences at one CPU are statistically inconclusive.

| Nodes | CPUs | Before | After | Time change |
| ---: | ---: | ---: | ---: | ---: |
| 10 | 1 | 2.952 ms | 2.984 ms | +1.07%, inconclusive |
| 10 | 4 | 2.471 ms | 2.507 ms | +1.47% |
| 500 | 1 | 40.211 ms | 40.134 ms | -0.19%, inconclusive |
| 500 | 4 | 33.045 ms | 33.306 ms | +0.79% |

The small cases allocate 596-696 extra bytes and six or seven additional objects per operation. The large cases' byte differences are inconclusive; the four-CPU case reports 5.5 additional allocations, or 0.00140%. Analysis creates an independent context and skips the final Go source check when no Go source was used.

The workload includes resolution, schema extraction, source emission, formatting, and publication of generated source files. Assertions verify the factory name, the two Go files, and their syntax. Emitted byte counts vary by up to two bytes across the samples, which use temporary source trees and numbered output directories. The comparison excludes Go module tidying and binary compilation.

Each side contains ten two-second samples at one and four CPUs, after the same correctness commands and one-iteration warmup. Both collections use Go 1.26.2 and the same checkout path, target, caches, and committed fixtures. Metadata identifies source revisions, commands, timestamps, input digests, and exit statuses. Validation recomputes the statistics and checks the pinned comparison output.
