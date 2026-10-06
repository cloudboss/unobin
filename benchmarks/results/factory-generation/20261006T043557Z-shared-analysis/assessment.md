# Shared analysis and factory generation

This compares `e87545ab2df76016f3a1372cc2c9be751717d43a` with `9b09c06d52797d5b20c61ae6958ae99236c30dc1`, using the baseline as the fixture revision. Both sides use identical 10- and 500-composite factories, Go 1.26.2, warm task-owned caches, ten two-second samples per case, and CPU settings 1 and 4. Correctness checks, warmups, all 40 samples per side, and source and fixture audits pass validation.

Each operation includes parsing, import resolution, compatibility and schema/type checks, UB and main Go emission, and publication into a fresh owned output directory. Fixture copying is outside timing. Generated Go syntax, exact source file inventory, and node counts are checked after timing. Go tidying and compilation are measured separately under `factory-build` and `factory-build-profiles`.

| Composites | CPU | Before, ms/op | After, ms/op | Time change | Allocated byte change | Allocation count change |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 10 | 1 | 3.969 | 3.049 | -23.18% | -26.01% | -29.49% |
| 10 | 4 | 3.304 | 2.536 | -23.24% | -25.99% | -29.49% |
| 500 | 1 | 52.068 | 41.245 | -20.79% | -21.98% | -26.13% |
| 500 | 4 | 42.060 | 34.152 | -18.80% | -21.91% | -26.13% |

All time, byte, and allocation-count reductions are significant at alpha=0.05. The primary assessment is improved, with no significant time regressions. Reusable import and package analysis, checked bodies, and typed generation inputs reduce repeated work. This comparison includes all changes between the source revisions and does not isolate the contribution of each change.

Generated source increases by 27 bytes: 6,262 to 6,289 bytes for 10 composites and 177,303 to 177,330 bytes for 500 composites. The common library setup and factory entry point account for the additional source. Both sides use the default full registry: state backends `local`, `s3`, and `gcs`; encryption types `env-key`, `kms`, `gcp-kms`, and `noop`.

Compile, source checking, generation, fixture validation, and compiled composite iteration checks pass on both sides. Allocated bytes measure cumulative work in the benchmark process; retained heap, peak process memory, and compiler subprocess memory were not measured. Complete samples, medians, confidence intervals, p-values, correctness logs, and metadata are included beside this assessment.
