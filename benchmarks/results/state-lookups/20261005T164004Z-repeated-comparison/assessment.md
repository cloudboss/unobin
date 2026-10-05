# Repeated state address comparison

This repeats the complete workload at
`f34c2f4c8b606ee7184aa52598a35c7ffa9ddee2` and
`fd515995afa30af311eea9ea53c99cb76b9f6d2a`, using the same preparation
revision, fixtures, toolchain, caches, commands, and capabilities. Both
correctness suites, warmups, all 200 samples per side, source audits, and
recomputed statistical output pass validation. The primary time assessment
remains **mixed**. The earlier comparison remains unchanged.

All state lookup cases improve significantly. Lookup-only gains range
from 75.70% to 99.74%, including construction of each operation's lookup.
The move/update/add/remove cases improve by 9.77% to 95.31%, including
snapshot cloning and index rebuilding after snapshot replacement.

The additional allocation costs reproduce the earlier comparison:
lookup-only operations add 3,576, 54,688, and 436,944 B/op at 100, 1,000,
and 10,000 entries. They add 5, 7, and 35 allocations. Operations with
changes add about 10,696, 164,032, and 1,310,799 B/op, plus 15, 21, and
105 allocations. The maps provide constant-time exact address lookup;
ordered removal still requires shifting the remaining positions.

| Control case | CPU | Time change | Assessment |
| --- | ---: | ---: | --- |
| Independent scheduler | 1 | +1.20% | Inconclusive |
| Independent scheduler | 4 | +2.69% | Regressed |
| Chain scheduler | 1 | +1.12% | Regressed |
| Chain scheduler | 4 | +4.66% | Regressed |
| Fan scheduler | 1 | +1.41% | Regressed |
| Fan scheduler | 4 | -4.33% | Improved |
| Checked-factory planner | 1 | +1.73% | Regressed |
| Checked-factory planner | 4 | +2.92% | Regressed |

These controls do not use state lookup. The independent CPU 4 median is
1.786 ms before and 1.834 ms after in this repeat, versus 2.085 ms and
2.743 ms in the earlier comparison. The smaller difference remains
significant; the magnitude of the earlier slowdown does not reproduce.
Changes in scheduler and planner timing have no established causal
attribution to the index. No performance gain is claimed for these controls.

The execution context adds two pointers. The checked-factory planner
and CPU 1 chain cases again add 16 B/op. CPU 1 independent adds 19.5 B/op;
variation beyond the context's size has no established cause. All raw
values, intervals, sample counts, and p-values remain in `summary.json`.

This supports the lookup improvement and its allocation costs. The
control cases require separate assessment when evaluating complete
planning or scheduling performance.
