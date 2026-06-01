// Package encrypt defines the contract a state-file encrypter
// implements.
//
// Encrypter values seal and unseal opaque byte slices. State backends
// receive an Encrypter from the runtime and call it once per snapshot
// read or write. The runtime uses the same encrypter for plan files.
// Encrypter implementations join the fixed set in pkg/backends; the
// env-key and no-op encrypters live in pkg/envencrypt because they have
// no SDK dependency.
package encrypt
