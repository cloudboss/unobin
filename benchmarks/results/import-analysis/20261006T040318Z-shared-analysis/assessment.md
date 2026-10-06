# Shared import and semantic analysis

This compares `e87545ab2df76016f3a1372cc2c9be751717d43a` with `4856555e2240b444484ee8ec9c6ae7e69761dc73`, using the baseline as the fixture revision. Both sides use identical workloads, Go 1.26.2, target settings, caches, and default capabilities. Correctness checks, warmups, all 200 samples per side, and source and fixture audits pass validation.

Nested-library and shared-export analysis is 48.46–54.22% faster. It allocates 47.55–52.09% fewer bytes and 48.77–51.33% fewer objects. These differences are significant at the declared 0.05 threshold.

| Workload | CPU | Before, milliseconds | After, milliseconds | Time change |
| --- | ---: | ---: | ---: | ---: |
| Two nested libraries | 1 | 3.0814 | 1.5806 | -48.71% |
| Two nested libraries | 4 | 2.5659 | 1.3225 | -48.46% |
| Five nested libraries | 1 | 5.6548 | 2.8675 | -49.29% |
| Five nested libraries | 4 | 4.6746 | 2.4056 | -48.54% |
| Ten nested libraries | 1 | 10.1027 | 5.0391 | -50.12% |
| Ten nested libraries | 4 | 8.2380 | 4.2153 | -48.83% |
| One shared export | 1 | 2.4174 | 1.2029 | -50.24% |
| One shared export | 4 | 1.9242 | 0.9890 | -48.60% |
| Ten shared exports | 1 | 17.4796 | 8.1315 | -53.48% |
| Ten shared exports | 4 | 13.7772 | 6.6324 | -51.86% |
| One hundred shared exports | 1 | 169.9422 | 77.7914 | -54.22% |
| One hundred shared exports | 4 | 134.6828 | 64.3657 | -52.21% |

Analysis shares one resolved import graph, Go package metadata, parsed UB bodies, and checked semantic data. Generation consumes the shared program data separately. Nested cases reduce UB source reads from 16 to 12, 34 to 24, and 64 to 44 per operation. Shared-export cases reduce reads from 10 to 8, 28 to 17, and 208 to 107. Resolver counts remain 3, 6, or 11 for nesting and two for shared exports; each operation still performs exactly one schema read.

The existing nested-library analysis/generation control is 44.86–47.56% faster and allocates about 47.8% fewer bytes. The factory-check control is 5.33–6.76% faster, with about 0.33% fewer bytes and 0.40% fewer allocations. These differences are significant.

Schema-cache controls retain freshness checks. One hundred unchanged lookups perform zero schema reads; changing a helper performs two reads. Their time improves by 2.29–6.43% and 1.20–7.22%, respectively. Helper-edit byte differences are inconclusive, and allocation counts are unchanged. The unchanged-source CPU 4 control allocates 765 additional bytes per operation, or 0.0134%; that byte difference is significant, with unchanged allocation count.

Each case has ten two-second samples at one and four CPUs after a symmetric one-iteration warmup. Import-analysis cases inject a counted schema reader and therefore exclude real Go schema derivation. Allocated bytes measure cumulative work; retained heap and process memory were not measured. The comparison includes all changes between the source revisions and does not isolate each contribution. Raw samples, exact statistics, confidence intervals, and command metadata accompany this assessment.
