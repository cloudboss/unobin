# Changelog

This changelog starts with v0.11.1. Earlier releases are listed in the [repository tags](https://github.com/cloudboss/unobin/tags).

## [Unreleased]

## [0.13.0] - 2026-11-22

### Added

- Add shared string and collection predicates, including `@core.is-match`, `@core.is-format`, `@core.has-key`, `@core.has-item`, and `@core.is-unique`. Go constraints can use computed operands and bound Unobin expressions.
- Add Go SDK constants for all formats accepted by `constraint.IsFormat`.
- Implement library API 1.1 for native constraints.

### Changed

- Extend `ForEach` in the Go SDK to support maps through `MapEntry[T]` callbacks exposing keys and values.
- Share asset resolution across builtin and Go execution boundaries.

### Fixed

- Preserve 64-bit integers when loading saved state so unchanged inputs do not plan a replacement because of rounding.
- Preserve negative zero in saved plans and state so an unchanged data-source response passes apply verification.
- Check required fields in evaluated Go resource, data source, and action inputs during planning and before apply operations, including replacement.
- Reject unknown fields in objects inside optional input collections during compilation, planning, and apply.
- Compare asset paths as logical strings and asset content by its bytes, including when the other operand is a byte list.
- Check resolved and defaulted data-source input constraints before planning reads, so lookup errors cannot mask constraint diagnostics.
- Check native constraints on known nested fields and list lengths during compilation and planning when other values in the same input are pending.
- Check list size limits when values are pending in lists forwarded through composites or built with projections and comprehensions.
- Check known keys and values in map comprehensions during planning when other values are pending.
- Preserve literal string escaping in extracted constraints and inherit constraint declarations through local and imported type aliases.
- Validate resolved and defaulted inputs during apply, including saved no-op plans and deferred resource decisions, before executing resource, data-source, or action operations. Historical state can still be destroyed when its inputs no longer satisfy current constraints.
- Keep plans bound to the exact state snapshot read during planning and reject stale plans after concurrent state changes.
- Recompute affected composite outputs and cached locals after reconciling resource outputs, including nested and for-each composites.
- Honor cancellation during S3 lock acquisition and bound lock-holder lookups after cancellation.
- Reject saved plans whose stack name does not match the runtime's state backend before acquiring a lock or writing state.

## [0.12.0] - 2026-10-07

> [!WARNING]
> This release contains breaking changes.
> - The state format has changed since v0.11.0.
> - The library API now has an internal versioning scheme.

### Added

- Add a changelog.
- Add test and release GitHub Actions workflows that integrate with the changelog for release notes.
- Enable bootstrapping of S3 and GCS state buckets. They can now be created on the first run if they don't exist.
- Declare independent library implementation APIs and check selected source before schema extraction and factory builds. Automatic dependency queries select the highest release whose final graph is compatible with the CLI.

### Changed

- Classify state entries by `category` and `composite` instead of `entry-kind`, including empty composite calls. Use the same classification in state list/show output. Earlier snapshots must be recreated.
- Move the `internal/e2etest` framework into `pkg/` for external library tests.
- Libraries now register typed resource definitions and receive prior target data through `runtime.Prior`. Existing libraries must be updated to include this change.
- Go library and configuration packages must declare their minimum implementation API in `Library().Compatibility`. Existing libraries must be updated to include this change.
- Enhance library resource replacement rules to use typed field selectors. Replacement rules can also now nest below top level fields.

### Fixed

- Avoid a panic when scanning object types with absent field declaration bodies.
- Remove obsolete owned UB packages and captured assets before building a factory. Reused output now resolves the same dependencies and factory identity as clean output. Earlier output directories require a fresh compilation destination.
- Preserve authored lifecycle implementations when regenerating Go libraries. Validate output ownership before publishing and remove only untouched obsolete lifecycle stubs.
- Reject saved plans after locally replaced Go implementation code changes. Factory identity includes linked source and build settings; replan after the first rebuild with this change.
- Keep sensitive values masked in destroy plans, including resources removed from configuration.
- Resolve resource decisions during apply when replacement or configuration rules depend on values that were unknown during planning.
- Keep dependent values pending until those resource decisions are resolved, including resources with empty outputs.

## [0.11.1] - 2026-09-06

### Changed

- Update the gRPC dependency.

### Fixed

- Preserve source locations in triple-string interpolation so imported libraries compile with accurate diagnostics.
- Defer composite outputs with unresolved inputs until apply can evaluate them.

[Unreleased]: https://github.com/cloudboss/unobin/compare/v0.13.0...HEAD
[0.13.0]: https://github.com/cloudboss/unobin/releases/tag/v0.13.0
[0.12.0]: https://github.com/cloudboss/unobin/releases/tag/v0.12.0
[0.11.1]: https://github.com/cloudboss/unobin/releases/tag/v0.11.1
