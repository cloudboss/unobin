# Ordered composite iteration lookup

This pair measures `2aa399b7cb8b268c976b192183faed4d753f957d` against
`c7fc3b8664860232a483d5ab9488439ed8a07f88`, using preparation revision
`313fba37db0044ef179009910f46e4da0a0889ed`. All nine cases, ten samples,
both CPU settings, fixed counts, correctness logs, source audits, and
recomputed comparison output passed validation. The primary time assessment
is **improved**, with no significant time regressions.

Each operation builds one ancestor lookup and ordered descendant lists.
All 100 boundary queries reuse those lists. Index construction is timed;
there is no cache between benchmark operations. The nested test also checks
membership when both an outer and an inner boundary iterate.

| Depth | CPU 1 time change | CPU 4 time change | Before bytes | After bytes | Before allocations | After allocations |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | -92.28% | -93.58% | 800 | 22,488 | 100 | 27 |
| 3 | -94.76% | -95.54% | 2,400 | 52,248 | 100 | 31 |
| 6 | -96.71% | -97.19% | 4,800 | 95,416 | 100 | 33 |

All iteration time improvements and byte increases are significant.
The byte increases are 2,711%, 2,077%, and 1,887.83%. The index requires
memory proportional to the composite ancestors and stored memberships;
its lists use one pointer allocation with exact capacities. This is the
cost of replacing repeated whole-graph ancestry scans with indexed queries.
The absolute increase is 21,688, 49,848, and 90,616 bytes per operation.
Allocation counts fall by 73%, 69%, and 67%. The additional memory funds
the lookup tables and descendant lists that produce the measured time gain.

DAG construction has no implementation change in this pair. Its time
measurements improve or are inconclusive; do not attribute those changes
to the iteration index. Its only significant byte regression is 4.5 B/op
at 1,000 composites and CPU 4, or 0.000234%. The cause of that small
shift is unestablished. All raw values remain in the structured summary.
Capabilities are identical on both sides.

Runtime, state, fixture-guard, and compiled nested-iteration checks passed
on both sides. The root short suite, vet, and runtime lint passed before
the implementation commit. The configured container gate remains
unavailable; the test logs are from the host Go 1.26.2 toolchain.
