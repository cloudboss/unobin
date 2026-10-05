# State address lookup

This comparison measures `f34c2f4c8b606ee7184aa52598a35c7ffa9ddee2`
against `fd515995afa30af311eea9ea53c99cb76b9f6d2a`. The preparation
revision is also `f34c2f4c8b606ee7184aa52598a35c7ffa9ddee2`. All ten
cases, ten samples per CPU setting, fixed counts, correctness logs,
source audits, and recomputed statistical output passed validation.
The primary time assessment is **mixed**.

Each operation builds an address-to-position map inside its execution
context. The lookup follows snapshot replacements. Updates retain their
positions, additions append, and removals shift the surviving positions
to preserve serialization order. Pruning and refresh invalidate the lookup.
Construction is timed, and each benchmark operation starts with a fresh
execution context. The public snapshot format and capabilities match.

| Entries | CPU 1 before, ms | CPU 1 after, ms | Time change | Before B/op | After B/op | Before allocations | After allocations |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 100 | 0.013023 | 0.003134 | -75.94% | 144 | 3,720 | 1 | 6 |
| 1,000 | 1.094385 | 0.032223 | -97.06% | 144 | 54,832 | 1 | 8 |
| 10,000 | 109.765992 | 0.363957 | -99.67% | 144 | 437,088 | 1 | 36 |

These cases perform one prior lookup per entry. CPU 4 time improvements
range from 85.15% to 99.74%. The extra 3,576, 54,688, and 436,944 bytes
per operation fund the map and execution context. The corresponding byte
increases are 2,483.33%, 37,977.78%, and 303,433.33% against a 144-byte
baseline. Allocation counts increase by 5, 7, and 35. These regressions
are significant at both CPU settings.

The changes cases prime a lookup, apply one actual leaf move, replace the
prior and active snapshots, update an entry, add an entry, remove an entry,
and look up both snapshots. They include cloning and three index builds.
Removals still take linear work to preserve ordered snapshots.

| Entries | CPU 1 time change | CPU 4 time change | Extra CPU 1 B/op | CPU 1 byte increase | Extra allocations |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 100 | -11.49% | -15.21% | 10,696 | 6.92% | 15 |
| 1,000 | -63.82% | -69.82% | 164,032 | 10.03% | 21 |
| 10,000 | -93.93% | -95.30% | 1,310,799 | 8.29% | 105 |

All time improvements and allocation increases in these cases are
significant. The content checks verify exact addresses, values, order,
and missing entries after moves and removals. Separate tests cover cache
freshness between operations, pruning, empty state, and escaped keys.

The independent scheduler case regresses by 3.07% at CPU 1 and 31.58% at
CPU 4, both significant. The cause of these time regressions is
unestablished. CPU 1 chain and fan differences are inconclusive; CPU 4
chain and fan times improve. Checked-factory planner time is inconclusive
at CPU 1 and improves at CPU 4. These cases do not exercise state lookups.

The execution context gains two pointers. This adds 16 B/op to the
checked-factory planner at both CPU settings and to the CPU 1 chain case.
Small scheduler byte shifts also appear: CPU 4 chain adds 42 B/op, CPU 1
fan adds 21.5 B/op, and CPU 1 independent adds 14.5 B/op. The additional
variation beyond the context size has no established cause. All values
and statistical bounds remain in the structured summary.

Runtime, state, fixture-guard, and compiled composite tests passed on both
sides. The root short suite, vet, runtime lint, and full runtime race suite
passed before the implementation commit. The configured container gate
remains unavailable; the logs use the host Go 1.26.2 toolchain.
