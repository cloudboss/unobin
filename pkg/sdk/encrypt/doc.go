// Package encrypt defines the contract a state-file encrypter
// implements.
//
// Encrypter values seal and unseal opaque byte slices. State backends
// receive an Encrypter from the runtime and call it once per snapshot
// read or write. The runtime uses the same encrypter for plan files.
// The implementations and the fixed set an operator selects from live
// together in pkg/encrypters.
package encrypt
