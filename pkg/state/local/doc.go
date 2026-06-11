// Package local stores state snapshots on the local filesystem.
//
// The Backend interface and Snapshot types live in pkg/sdk/state so
// provider libraries can implement their own backends without
// depending on unobin core. The fixed set an operator selects from
// lives in pkg/backends.
package local
