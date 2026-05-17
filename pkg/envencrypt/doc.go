// Package envencrypt holds unobin's built-in encrypters.
//
// The Encrypter contract lives in pkg/sdk/encrypt so provider modules
// can register their own implementations. This package implements the
// env-key encrypter and the no-op pass-through. The core module
// registers them under runtime.Module.Encrypters.
package envencrypt
