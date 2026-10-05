# Combined composite lookup changes

This comparison measures original preparation revision
`313fba37db0044ef179009910f46e4da0a0889ed` against final composite-index
revision `c7fc3b8664860232a483d5ab9488439ed8a07f88`. It includes direct
child collection and operation-local iteration indexes. The separate
direct-child and iteration comparisons remain available. All nine cases,
ten samples, both CPU settings, fixed counts, correctness logs, source
audits, and recomputed comparison output passed validation.

All measured time cases improve significantly. At 8,000 composites,
DAG construction changes from 1.800 s to 15.15 ms at CPU 1 (-99.16%)
and from 1.829 s to 11.37 ms at CPU 4 (-99.38%). At CPU 1, growth
from 1,000 to 8,000 composites is 1.55 ms to 15.15 ms after indexing,
compared with 20.01 ms to 1.800 s before indexing. Direct child lookup
does not scan the entire node map for each boundary.

Iteration time improves by 91.96-97.17% across depths and CPU settings.
The earlier direct-child pair's unchanged iteration cases included small
time regressions. This combined pair replaces those queries with indexed
lookup and demonstrates gains against the original baseline; it does not
establish a cause for the earlier unchanged-query shifts.

Iteration bytes increase from 800/2,400/4,800 to
22,488/52,248/95,416 B/op for depths 1/3/6. Allocation counts fall from
100 to 27/31/33. The index is rebuilt inside every measured operation,
stores only iteration boundary memberships, and uses one exact-capacity
pointer allocation for all result lists. This bounded memory cost enables
the measured time gain. Acceptance of that tradeoff remains a review item;
this comparison does not declare the whole F06 work package complete.

Three DAG byte measurements also increase significantly at CPU 4:
10.5 B/op at 100 composites, 6.5 B/op at 500, and 20.5 B/op at 2,000.
Each is below 0.01%. Their cause is unestablished. The full values and
all secondary metrics are retained in the summary. There are no capability
changes or significant allocation-count regressions.

Runtime, state, fixture-guard, and compiled nested-iteration checks passed.
The root short suite, vet, and runtime lint passed before implementation
commits. Container validation remains unavailable; these are host checks.
