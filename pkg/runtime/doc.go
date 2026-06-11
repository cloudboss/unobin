// Package runtime is the execution engine linked into every compiled factory
// binary.
//
// Owns:
//   - DAG construction (implicit deps via reference; explicit via @depends-on)
//   - Plan computation: refresh + drift + change detection + replace-because chains
//   - Apply execution: parallelism cap (default 10), per-resource state writes,
//     apply error UX
//   - State model (snapshots, content-addressed, encrypted at rest)
//   - Action semantics (triggered with @trigger; 'always' literal; @lock; @timeout)
//
// Companion packages:
//   - pkg/sdk/state - Backend contract that provider libraries implement
//   - pkg/state/local and pkg/state/s3 - the filesystem and S3 backends
//   - pkg/runner - the factory CLI that invokes runtime entry points
package runtime
