// Package envencrypt holds unobin's built-in encrypters.
//
// The Encrypter contract lives in pkg/sdk/encrypt so provider libraries
// can register their own implementations. This package implements the
// env-key encrypter and the no-op pass-through. The core library
// registers them under runtime.Library.Encrypters.
package envencrypt
