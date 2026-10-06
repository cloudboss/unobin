# Instance dependency candidates

This comparison measures `e8cc08ff02b704350ff83d7ffb000251662e1134` against `97038ffdb85680c885b09e3d8f1b342b9e69c78f`, with preparation revision `178bcc8ad0868007f5599b116e179cce604590cd`. All 16 cases, ten samples per CPU setting, fixed counts, correctness checks, source audits, and recomputed statistical output passed validation. The primary time assessment is **improved**. No metric has a significant regression.

The builder parses each step address once and analyzes each declaration body once. It selects candidates from key indexes while preserving shared ancestor agreement, conservative matching across several iteration levels, dependency order, destroy ordering, orphans, and locks. The indexes belong to one graph construction. Their construction is included in the measurement.

| Pairs | CPU 1 before, ms | CPU 1 after, ms | Time change | Before B/op | After B/op |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 100 | 5.660 | 0.107 | -98.10% | 4,303,639 | 153,672 |
| 250 | 34.016 | 0.278 | -99.18% | 25,860,628 | 459,224 |
| 500 | 134.381 | 0.560 | -99.58% | 101,722,037 | 923,504 |
| 1,000 | 535.500 | 1.100 | -99.79% | 403,431,620 | 1,840,816 |
| 2,000 | 2,133.381 | 2.368 | -99.89% | 1,606,924,664 | 3,712,304 |

Each paired case has twice as many steps as pairs and exactly one edge per pair. Increasing the input 20 times increases median CPU 1 time 22 times after the change, compared with 377 times before it. At 2,000 pairs, allocations fall from 64,148,194 to 34,163 per operation. CPU 4 paired times improve by 98.19% to 99.89%; its largest case takes 1.924 ms.

The nested cases retain 200 steps and 100 edges at depths 1, 3, and 6. Times improve by 97.98% to 98.65% across both CPU settings. CPU 1 bytes fall from 4,303,639 to 153,672 at depth 1, from 9,431,647.5 to 240,120 at depth 3, and from 25,276,442 to 380,984 at depth 6. The tests check missing shared keys, matching at any dependency key position, repeated keys, remaining ancestor constraints, and fresh analysis after body changes.

Cartesian cases retain all 100, 625, 2,500, and 10,000 expected edges. Times improve by 65.19% to 70.17%, while CPU 1 bytes improve by 55.44% to 67.36%. The largest case uses 1,031,120 B/op and 3,048 allocations after the change. Producing those edges still requires work proportional to their count. Step bindings are omitted in the graph benchmarks to isolate dependency construction; the workload metadata states this limit.

The existing independent, chain, and fan scheduler cases improve in time, bytes, and allocations at both CPU settings. Their time gains range from 1.51% to 18.06%. The checked-factory planner's implementation is unchanged: its CPU 1 time shifts by -1.84%, its CPU 4 difference is inconclusive, and its bytes and allocations are unchanged. The small time shift has no established connection to the dependency index. Capabilities match.

Runtime, state, fixture-guard, and compiled composite checks passed on both sides. The root short suite, vet, runtime lint, and focused scheduler race tests passed before the implementation commit. The configured container gate remains unavailable; the logs use the host Go 1.26.2 toolchain.
