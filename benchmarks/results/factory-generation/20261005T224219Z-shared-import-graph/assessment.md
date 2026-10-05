# Factory generation with a shared import graph

Factory generation is 21.43–25.62% faster and allocates 21.84–26.04% fewer bytes
with import graph reuse. This workload includes resolution, schema extraction,
source emission, formatting, and publication of generated source files.

| Nodes | CPUs | Before | After | Time change |
| ---: | ---: | ---: | ---: | ---: |
| 10 | 1 | 3.969 ms | 2.952 ms | -25.62% |
| 10 | 4 | 3.304 ms | 2.471 ms | -25.20% |
| 500 | 1 | 52.068 ms | 40.211 ms | -22.77% |
| 500 | 4 | 42.060 ms | 33.045 ms | -21.43% |

Allocation counts fall by 26.13–29.55%. The benchmark verifies the factory name,
the two generated Go files, and their syntax. Emitted byte counts vary by one
byte in the samples. The benchmark uses temporary source trees and numbered
output directories. This comparison excludes Go module tidying and binary
compilation.

Each side contains ten two-second samples at one and four CPUs, after the same
correctness commands and one-iteration warmup. Both collections used the same
checkout path, Go 1.26.2, target, caches, and committed fixtures. Metadata includes
source revisions, commands, timestamps, input digests, and exit statuses. The
validator recomputes the statistics and checks the pinned comparison output.
