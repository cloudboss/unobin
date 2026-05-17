// Package state holds unobin's local state backend and the built-in
// encrypters.
//
// The Backend interface and Snapshot types live in pkg/sdk/state so
// provider modules can implement their own backends without depending
// on unobin core. This package implements only the local-filesystem
// backend, which the core module registers as the default.
package localstate
