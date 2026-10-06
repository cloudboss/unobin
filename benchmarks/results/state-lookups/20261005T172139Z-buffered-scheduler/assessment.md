# State lookup and apply scheduling

This compares `f34c2f4c8b606ee7184aa52598a35c7ffa9ddee2` with `2d397e996c9c4ac45956466c005011192e25d7a0`. It includes the execution-local state address index and the scheduler's bounded result queue and suppression of unused start notifications. The preparation revision, fixtures, commands, toolchain, caches, and capabilities match. Both correctness suites, warmups, all 200 samples per side, source audits, and recomputed statistical output pass validation.

All state lookup cases improve significantly. Lookup-only operations are 76.69% to 99.73% faster, including construction of the address index. Operations with a move, update, addition, and removal are 12.09% to 95.45% faster, including snapshot cloning and index rebuilding.

The index's allocation costs remain explicit. At 100, 1,000, and 10,000 entries, lookup-only operations add 3,576, 54,688, and 436,944 B/op, plus 5, 7, and 35 allocations. Operations with changes add approximately 10,696, 164,032, and 1,310,799 B/op, plus 15, 21, and 105 allocations. Exact address lookup uses a map; ordered removal still shifts positions.

| Scheduler case | CPU | Time change | Allocated byte change |
| --- | ---: | ---: | ---: |
| Independent | 1 | -31.71% | -17.19% |
| Independent | 4 | -27.92% | -17.20% |
| Chain | 1 | -21.55% | -16.22% |
| Chain | 4 | -16.45% | -16.21% |
| Fan | 1 | -27.60% | -16.41% |
| Fan | 4 | -30.85% | -16.41% |

All scheduler time and allocation improvements are significant. These cases have no event consumer. Omitting start messages avoids copying each step for an unused notification. The bounded queue lets a worker submit results without waiting for the scheduler to receive each message. The worker count, dependencies, locks, enabled events, failure handling, and draining retain their tested behavior.

The checked-factory planner control does not use state lookup or execute the scheduler. Its CPU 1 time change is +0.66% and inconclusive. Its CPU 4 median increases from 613.08 to 629.20 microseconds (+2.63%, p=0.035), a significant regression with no established causal attribution. Both CPU settings add 16 B/op for the larger execution context and retain the same allocation count. No planning speed improvement is claimed for this case.

The complete workload therefore has a mixed primary assessment. Raw samples, confidence intervals, sample counts, and p-values remain in `summary.json` and the associated comparison files. Earlier comparisons remain unchanged.
