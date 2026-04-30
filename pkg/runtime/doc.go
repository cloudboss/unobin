// Package runtime is the execution engine linked into every compiled stack
// binary.
//
// Owns:
//   - DAG construction (implicit deps via reference; explicit via @depends-on)
//   - Plan computation: refresh + drift + change detection + replace-because chains
//   - Apply execution: parallelism cap (default 10), per-resource state writes,
//     @on-completion hook, apply error UX
//   - State model (snapshots, content-addressed, encrypted at rest)
//   - Action semantics (triggered with @trigger; 'always' literal; @lock; @timeout)
//
// Companion packages:
//   - pkg/state - backend implementations (local, s3, gcs, azure-blob)
//   - pkg/cli/stack - CLI surface that invokes runtime entry points
package runtime
