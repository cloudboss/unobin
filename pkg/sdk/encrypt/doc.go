// Package encrypt defines the contract a state-file encrypter
// implements.
//
// Encrypter values seal and unseal opaque byte slices. State backends
// receive an Encrypter from the runtime and call it once per snapshot
// read or write. The runtime uses the same encrypter for plan files.
// Provider modules register their own encrypters under
// runtime.Module.Encrypters; the env-key encrypter lives in
// pkg/envencrypt because it has no SDK dependency.
package encrypt
