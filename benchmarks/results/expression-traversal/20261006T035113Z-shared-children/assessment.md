# Shared expression traversal

This compares `b826f50c44ef9d07fd3ab835c913a3a78ddbfdc4` with `4de24d2c9be92cea2852751bb4666c58f3f2b370`, using the before revision as the fixture revision. Both sides use identical expressions, reference-checking fixtures, Go 1.26.2, cache settings, and default capabilities. Correctness checks, warmups, all 140 samples per side, and source and fixture audits pass validation.

`Walk` and `ScanExpr` now share child traversal. Tests compare complete visit sequences on the same nested-expression fixtures, all type-expression containers, and declaration bodies. Nested comprehension bindings, source-before-binding order, child skipping, and stop decisions retain their behavior. The scanner also avoids a nil-pointer panic on absent object-type declaration bodies.

| Fields | API | CPU | Before, microseconds | After, microseconds | Time change |
| ---: | --- | ---: | ---: | ---: | ---: |
| 10 | Walk | 1 | 0.6245 | 0.6401 | +2.50% |
| 10 | Walk | 4 | 0.6243 | 0.6468 | +3.60% |
| 100 | Walk | 1 | 6.2235 | 6.4305 | +3.33% |
| 100 | Walk | 4 | 6.2200 | 6.4720 | +4.05% |
| 1,000 | Walk | 1 | 73.7285 | 76.1725 | +3.31% |
| 1,000 | Walk | 4 | 72.9455 | 76.6145 | +5.03% |
| 10 | ScanExpr | 1 | 1.1405 | 1.0375 | -9.03% |
| 10 | ScanExpr | 4 | 1.1250 | 1.0190 | -9.42% |
| 100 | ScanExpr | 1 | 10.6720 | 9.7055 | -9.06% |
| 100 | ScanExpr | 4 | 10.7935 | 9.7195 | -9.95% |
| 1,000 | ScanExpr | 1 | 119.5020 | 109.3680 | -8.48% |
| 1,000 | ScanExpr | 4 | 119.3230 | 109.4440 | -8.28% |

All traversal time differences are significant at the declared 0.05 threshold. The primary assessment is mixed: scanner traversal improves, while direct walks take longer. Shared traversal selects the visit mode at each expression. The scanner removes a separate recursive dispatch layer; the direct walker gains the mode selection. These measurements do not isolate the exact cost of that selection.

`Walk` remains at zero bytes and zero allocations per operation. `ScanExpr` remains at 32 bytes and two allocations. Every operation visits exactly 161, 1,601, or 16,001 expressions, with parsing and tree construction outside timing.

Reference checking over 400 composite scopes changes from 1.10663 to 1.09764 milliseconds at CPU 1 and from 1.19303 to 0.95203 milliseconds at CPU 4. Both time differences are inconclusive, with p-values of 0.1655 and 0.1051. The benchmark checks that reference analysis returns no diagnostics.

Reference allocation counts remain at 14,151 and 14,154 per operation at the two CPU settings. Median allocated bytes increase by one byte in each case. The byte difference is significant only at CPU 1 (p=0.0495); CPU 4 is inconclusive.

Each case has ten two-second samples at one and four CPUs after a symmetric one-iteration warmup. Allocation counts measure cumulative work, not retained heap or process memory. Raw samples, exact statistics, confidence intervals, and command metadata accompany this assessment.
