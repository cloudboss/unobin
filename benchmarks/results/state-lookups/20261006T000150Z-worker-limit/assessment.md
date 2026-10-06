# Apply scheduling with the worker limit

This compares `2d397e996c9c4ac45956466c005011192e25d7a0` with
`688939ce16fe7011920f19b2d6fcf8d6ffdfda99`. The scheduler now counts
dispatched operations against the worker limit until their results are
received. This prevents a serial apply from starting the next operation
before it processes a queued failure. Buffered results remain enabled.

The fixture revision is `f34c2f4c8b606ee7184aa52598a35c7ffa9ddee2`.
Fixtures, commands, toolchain, caches, and capabilities match. Both
correctness suites, warmups, all 200 samples per side, source audits,
and recomputed statistical output pass validation. The after revision
also contains compiler changes that do not run in these benchmarks.

| Scheduler case | CPU | Time change | Allocated byte change | Allocation count change |
| --- | ---: | ---: | ---: | ---: |
| Independent | 1 | -2.93% | -0.006% | -0.046% |
| Independent | 4 | -5.04% | -0.91% | -7.28% |
| Chain | 1 | -3.08% | 0% | 0% |
| Chain | 4 | +4.30% | -0.002% | 0% |
| Fan | 1 | -1.41% | -0.013% | -0.096% |
| Fan | 4 | -5.35% | -0.89% | -6.59% |

All scheduler time differences are significant. The CPU 4 chain median
increases from 1.434 to 1.495 milliseconds. Its byte difference is
inconclusive and its allocation count is unchanged. The worker limit
is required for failure handling; these results do not support a claim
that the correction improves every scheduler case.

The state lookup and checked-factory planning implementations are
unchanged in this pair. Lookup-only medians decrease by 0.69% to 4.21%;
planning medians decrease by 2.33% to 2.69%. These differences have no
established causal attribution to the scheduler correction.

The changed-snapshot control at 1,000 entries and CPU 1 increases from
1.155 to 1.161 milliseconds (+0.54%, p=0.0232). At CPU 4, its byte
median increases by 0.5 B/op; the 10,000-entry case adds 1 B/op. Both
byte differences are significant, while allocation counts remain
unchanged. Other changed-snapshot time differences range from -5.70%
to +0.74%; the 1,000-entry CPU 4 and both 10,000-entry differences
are inconclusive.

The complete workload has a mixed primary assessment. Exact medians,
confidence intervals, sample counts, and p-values are in
`summary.json` and the raw comparison files. Earlier comparisons
remain unchanged.
