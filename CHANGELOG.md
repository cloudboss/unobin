# Changelog

This changelog starts with v0.11.1. Earlier releases are listed in the [repository tags](https://github.com/cloudboss/unobin/tags).

## [Unreleased]

### Added

- Add a changelog.
- Add test and release GitHub Actions workflows that integrate with the changelog for release notes.
- Enable bootstrapping of S3 and GCS state buckets. They can now be created on the first run if they don't exist.
- Declare independent library implementation APIs and check selected source before schema extraction and factory builds. Automatic dependency queries select the highest release whose final graph is compatible with the CLI.

### Changed

- Move the `internal/e2etest` framework into `pkg/` for external library tests.
- Libraries now register typed resource definitions and receive prior target data through `runtime.Prior`. Existing libraries must be updated to include this change.
- Go library and configuration packages must declare their minimum implementation API in `Library().Compatibility`. Existing libraries must be updated to include this change.
- Enhance library resource replacement rules to use typed field selectors. Replacement rules can also now nest below top level fields.

### Fixed

- Reject saved plans after locally replaced Go implementation code changes.
  Factory identity includes linked source and build settings; replan after the
  first rebuild with this change.
- Keep sensitive values masked in destroy plans, including resources removed from configuration.
- Resolve resource decisions during apply when replacement or configuration rules depend on values that were unknown during planning.
- Keep dependent values pending until those resource decisions are resolved, including resources with empty outputs.

## [0.11.1] - 2026-09-06

### Changed

- Update the gRPC dependency.

### Fixed

- Preserve source locations in triple-string interpolation so imported libraries compile with accurate diagnostics.
- Defer composite outputs with unresolved inputs until apply can evaluate them.

[Unreleased]: https://github.com/cloudboss/unobin/compare/v0.11.1...HEAD
[0.11.1]: https://github.com/cloudboss/unobin/releases/tag/v0.11.1
