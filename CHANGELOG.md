# Changelog

This changelog starts with v0.11.1. Earlier releases are listed in the
[repository tags](https://github.com/cloudboss/unobin/tags).

## [Unreleased]

### Added

- Add a release workflow that tests and builds the CLI before publishing Linux
  and macOS archives for amd64 and arm64, with SHA-256 checksums.
- Add shared CI checks, release commands, and a maintained changelog.
- Add the public `pkg/e2etest` framework for compiled external library tests.

### Changed

- Resource libraries now register typed resource definitions and receive prior
  target data through `runtime.Prior`. Library implementations must migrate to
  these APIs.
- Define replacement rules with typed field selectors and check recorded
  replacement decisions before mutations.

### Fixed

- Keep sensitive values masked in destroy plans, including resources removed
  from configuration.
- Resolve resource decisions during apply when replacement or configuration
  rules depend on values that were unknown during planning.
- Keep dependent values pending until those resource decisions are resolved,
  including resources with empty outputs.

## [0.11.1] - 2026-09-06

### Changed

- Update the gRPC dependency.

### Fixed

- Preserve source locations in triple-string interpolation so imported
  libraries compile with accurate diagnostics.
- Defer composite outputs with unresolved inputs until apply can evaluate them.

[Unreleased]: https://github.com/cloudboss/unobin/compare/v0.11.1...HEAD
[0.11.1]: https://github.com/cloudboss/unobin/releases/tag/v0.11.1
