# Default factory build comparison

Both revisions build the full registry: state backends `local`, `s3`, and `gcs`; encryption types `env-key`, `kms`, `gcp-kms`, and `noop`. The default capabilities are unchanged. The source revisions are `e87545ab2df76016f3a1372cc2c9be751717d43a` and `c844a810d636068ba1512f3a5154539424421901`; the former also supplies the unchanged benchmark fixtures.

The comparison uses identical 10- and 500-node factories, Go 1.26.2, CPU=1, linker options, module cache, and five independent one-build samples per case. Cold cases start each build with an empty Go build cache; warm cases reuse the cache populated by the symmetric warmup. Each build still runs resolution, checking, generation, module tidying, and Go compilation.

| Nodes | Cache | Before, s/build | After, s/build | Change | p |
| ---: | --- | ---: | ---: | ---: | ---: |
| 10 | warm | 1.528 | 1.603 | +4.89% | 0.841 |
| 10 | cold | 11.054 | 11.043 | -0.10% | 0.421 |
| 500 | warm | 1.815 | 1.908 | +5.13% | 0.421 |
| 500 | cold | 11.472 | 11.238 | -2.04% | 0.095 |

None of the build-time differences is significant at alpha=0.05. Five samples satisfy the whole-build policy but do not provide finite 95% confidence bounds. These results do not establish a timing improvement.

| Nodes | Cache | B/build, before | B/build, after | Change | Allocations, before | Allocations, after | Change |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 10 | warm | 84,069,840 | 84,052,488 | -0.02% | 167,743 | 162,543 | -3.10% |
| 10 | cold | 84,070,896 | 84,052,600 | -0.02% | 167,749 | 162,547 | -3.10% |
| 500 | warm | 110,761,016 | 105,230,200 | -4.99% | 658,551 | 526,427 | -20.06% |
| 500 | cold | 110,762,104 | 105,230,328 | -4.99% | 658,556 | 526,429 | -20.06% |

The 500-node cases use about 5.0% fewer allocated bytes and 20.1% fewer allocations. These metrics measure the benchmark process; they exclude the memory used by the Go compiler subprocesses and do not measure peak or retained heap.

| Nodes | Cache | Binary bytes, before | Binary bytes, after | Source bytes, before | Source bytes, after | Packages, before | Packages, after |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 10 | warm | 48,798,438 | 48,857,375 | 6,267 | 6,294 | 715 | 718 |
| 10 | cold | 48,798,438 | 48,857,375 | 6,267 | 6,294 | 715 | 718 |
| 500 | warm | 49,598,399 | 49,657,896 | 177,308 | 177,335 | 715 | 718 |
| 500 | cold | 49,598,399 | 49,657,984 | 177,308 | 177,334 | 715 | 718 |

The full registry adds three packages and about 59 KB to the binaries (0.12%). Generated source grows by 26-27 bytes. Temporary build identity values explain small sample differences in emitted or binary bytes.

Correctness checks pass on both sides for compile, source checking, generation, fixture validation, and the compiled composite iteration consumer. Each sample also verifies the generated Go files, compiled binary, and build identity. Live cloud operations are outside this comparison; backend and encryption contract tests cover registration.
