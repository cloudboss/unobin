// Package state defines the contract a state backend implements.
//
// A backend stores snapshots, advances a current pointer, and arbitrates
// a per-stack lock. A backend implementation satisfies the Backend
// interface and joins the fixed set in pkg/backends, where an operator
// selects it by name. Encryption is a separate concern; see the sibling
// pkg/sdk/encrypt package.
package state
