# Full and local factory build profiles

Both profiles use source and fixture revision
`c844a810d636068ba1512f3a5154539424421901`, identical 10- and 500-node
factories, Go 1.26.2, CPU=1, linker options, downloaded modules, and five
one-build samples per case. The selected profile is the only environment
difference. Warm cases reuse the Go build cache; each cold sample starts
with an empty build cache. Setup and module downloads are outside timing.

| Profile | State backends | Encryption types |
| --- | --- | --- |
| Full (default) | `local`, `s3`, `gcs` | `env-key`, `kms`, `gcp-kms`, `noop` |
| Local | `local` | `env-key`, `noop` |

The local profile has fewer built-in capabilities. Its results describe
that explicit choice and do not establish a faster default build. Provider
libraries can still import their own cloud dependencies.

| Nodes | Cache | Full, s/build | Local, s/build | Change |
| ---: | --- | ---: | ---: | ---: |
| 10 | warm | 1.575 | 0.796 | -49.49% |
| 10 | cold | 11.058 | 6.984 | -36.84% |
| 500 | warm | 1.847 | 1.080 | -41.56% |
| 500 | cold | 11.410 | 7.052 | -38.19% |

Build times fall by 36.8-38.2% with a cold cache and 41.6-49.5% with a
warm cache. All four comparisons have p=0.008 at alpha=0.05. Five samples
satisfy the whole-build policy but do not provide finite 95% confidence
bounds. No universal build-time guarantee follows from these fixtures.

| Nodes | Cache | Binary bytes, full | Binary bytes, local | Source bytes, full | Source bytes, local | Packages, full | Packages, local |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 10 | warm | 48,857,399 | 16,022,712 | 6,296 | 6,307 | 718 | 380 |
| 10 | cold | 48,857,399 | 16,022,688 | 6,296 | 6,308 | 718 | 380 |
| 500 | warm | 49,657,896 | 16,827,561 | 177,337 | 177,349 | 718 | 380 |
| 500 | cold | 49,657,896 | 16,827,553 | 177,337 | 177,349 | 718 | 380 |

Local binaries are 66.1-67.2% smaller, and the dependency inventory falls
from 718 packages to 380. Source grows by 11-12 bytes because of the
generated entry point. Each local sample checks that its dependencies
exclude the full-registry facade and the AWS and Google cloud SDKs.

| Nodes | Cache | B/build, full | B/build, local | Change | Allocations, full | Allocations, local | Change |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 10 | warm | 84,055,312 | 38,180,472 | -54.58% | 162,545 | 115,595 | -28.88% |
| 10 | cold | 84,055,408 | 38,180,552 | -54.58% | 162,547 | 115,597 | -28.88% |
| 500 | warm | 105,283,024 | 59,422,392 | -43.56% | 526,428 | 479,474 | -8.92% |
| 500 | cold | 105,283,152 | 59,422,136 | -43.56% | 526,431 | 479,473 | -8.92% |

The benchmark process allocates 43.6-54.6% fewer bytes and 8.9-28.9%
fewer objects. These counts exclude Go compiler subprocess memory and do
not measure peak or retained heap.

Both sides pass compile, source checking, generation, fixture validation,
and compiled composite iteration checks. The compiled profile consumer
also verifies full-registry schema exposure, local plan/apply/output/state
commands, distinct build identities, and unsupported cloud selections
failing before local state creation. Every sample validates generated Go
files, the compiled binary, and its build identity. Live cloud operations
are outside this comparison.
