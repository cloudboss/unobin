# Diagnostics after queued editor changes

The comparison improves latest-version diagnostic latency in every measured
case. Each operation sends eight complete document changes without waiting
between changes, then waits for diagnostics at the latest version. The checks
verify the URI, increasing diagnostic versions, and absence of errors.

| Nodes | CPUs | Median latency before | Median latency after | Latency change | Allocated bytes change | Allocation count change |
| --- | --- | --- | --- | --- | --- | --- |
| 50 | 1 | 959.9 ms | 234.7 ms | -75.55% | -74.97% | -75.00% |
| 50 | 4 | 728.63 ms | 95.11 ms | -86.95% | -86.42% | -86.44% |
| 500 | 1 | 1738.8 ms | 244.8 ms | -85.92% | -84.62% | -84.75% |
| 500 | 4 | 1200.5 ms | 192.2 ms | -83.99% | -82.02% | -82.11% |

All latency, allocated-byte, and allocation-count differences are statistically
significant at the declared 0.05 threshold. Both revisions have ten samples per
case and CPU setting, for 40 samples on each side. The node counts and eight
edits per operation match on both sides.

The baseline publishes all eight versions. The implementation publishes the
latest version once in every measured burst. It coalesces pending work and
cancels obsolete work, with at most two diagnostic workers. Publication count
does not establish the number of analyses started: obsolete work can begin
before a newer change arrives. Other event timing can permit intermediate
versions to finish and publish while they are still current.

The comparison includes request cancellation, concurrent protocol output,
shared syntax preparation, and diagnostic scheduling changes. It does not
isolate their individual costs. JSON encoding, framing, transport, analysis,
and publication are timed. Initial document diagnostics and connection cleanup
are outside timing. Allocated bytes measure cumulative allocations, rather
than retained heap or process memory. This workload does not measure unchanged
feature requests, cold opens, or dependency invalidation.

The fixture revision and baseline are
`a834721da4d8d088efdeb7e5fcdd7d18d3df2a06`. The implementation is
`6d0abd1d449952fcc0345d169e4bfdf5af9d9f56`. Collection correctness checks,
warmups, source and fixture audits, and statistical recomputation passed on
both sides. Raw samples, commands, metadata, and statistical warnings are
included beside this assessment.
