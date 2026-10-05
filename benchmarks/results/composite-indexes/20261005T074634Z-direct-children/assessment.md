# Direct composite child lookup

This comparison measures source revision
`313fba37db0044ef179009910f46e4da0a0889ed` against
`2aa399b7cb8b268c976b192183faed4d753f957d`. The benchmark preparation
revision is the former. All nine cases, ten samples, both CPU settings,
fixed node/edge counts, correctness checks, and source audits passed.
The pinned comparison output and recomputed summary also passed validation.

All DAG construction time cases improve with statistical significance.
Median changes at CPU settings 1 and 4 are:

| Composites | CPU 1 | CPU 4 |
| --- | ---: | ---: |
| 100 | -58.23% | -48.91% |
| 500 | -88.29% | -82.58% |
| 1,000 | -92.02% | -91.18% |
| 2,000 | -95.50% | -96.08% |
| 4,000 | -98.07% | -98.50% |
| 8,000 | -99.16% | -99.36% |

At CPU 1, the 1,000-to-8,000 series grows from 20.01 ms to 1.80 s
before the change and from 1.60 ms to 15.21 ms after it. At CPU 4,
the after series grows from 1.30 ms to 11.65 ms. The implementation
collects direct children in one pass and sorts each boundary's children.
The measured graph has one child per boundary. Its boundary lookup no
longer scans all graph nodes once per composite.

The full primary-metric assessment remains **mixed**. Iteration lookup has
no implementation change in this pair. Its depth-1 medians increase by
4.20% at CPU 1 and 2.94% at CPU 4, and its depth-6 median increases by
0.62% at CPU 1, all with statistical significance. The other iteration
time changes are inconclusive. A cause for these shifts has not been
established; identical query code alone does not prove identical costs
for differently constructed graphs. Do not claim an iteration improvement
or mark the complete composite-index work done from this result.

Three byte measurements increase significantly: 11 B/op for 100
composites at CPU 4, 7.5 B/op for 500 at CPU 4, and 21.5 B/op for 2,000
at CPU 4. Each increase is below 0.01%. Other byte changes are improvements
or inconclusive; allocation-count changes are improvements, unchanged,
or inconclusive. Iteration bytes and allocation counts are unchanged.
These values are retained without rounding them out of the structured
summary. This pair changes no capabilities.

Runtime, state, fixture-guard, and compiled nested-composite tests passed.
The correctness logs accompany each side. The root short suite,
vet, and the affected package's linter passed before the implementation
commit. The configured container integration gate was unavailable; these
checks used the host Go 1.26.2 toolchain.
