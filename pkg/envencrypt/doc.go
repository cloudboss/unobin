// Package envencrypt holds unobin's built-in encrypters.
//
// The Encrypter contract lives in pkg/sdk/encrypt. This package
// implements the env-key encrypter and the no-op pass-through; pkg/backends
// puts them in the fixed set an operator selects by name.
package envencrypt
