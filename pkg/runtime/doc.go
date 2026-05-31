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
//   - pkg/localstate - the local filesystem backend (the only one in core)
//   - pkg/runner - the factory CLI that invokes runtime entry points
package runtime
