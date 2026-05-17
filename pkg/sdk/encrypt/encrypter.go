package encrypt

// Encrypter seals and unseals opaque bytes. Implementations cover one
// key source each: an env var holding a 32-byte symmetric key, a KMS
// service that wraps a per-snapshot data key, and so on.
type Encrypter interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}
