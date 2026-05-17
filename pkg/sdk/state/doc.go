// Package state defines the contract a state backend implements.
//
// A backend stores snapshots, advances a current pointer, and arbitrates
// a per-deployment lock. Provider modules import this package, satisfy
// the Backend interface, and register their implementations under
// runtime.Module.StateBackends. Encryption is a separate concern; see
// the sibling pkg/sdk/encrypt package.
package state
