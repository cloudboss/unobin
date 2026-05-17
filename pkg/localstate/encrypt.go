package localstate

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

// Encrypter encrypts and decrypts opaque bytes. Implementations cover one
// key source each: an env var holding a 32-byte symmetric key, a KMS
// service that wraps a per-snapshot data key, etc.
type Encrypter interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// NoopEncrypter passes bytes through unchanged. Useful in tests and for
// dev workflows where the operator has explicitly opted out.
type NoopEncrypter struct{}

func (NoopEncrypter) Encrypt(p []byte) ([]byte, error) { return p, nil }
func (NoopEncrypter) Decrypt(p []byte) ([]byte, error) { return p, nil }

// EnvKeyEncrypter uses AES-256-GCM with a 32 byte symmetric key read from
// a named environment variable. The env value must be the base64 encoded key.
type EnvKeyEncrypter struct {
	aead cipher.AEAD
}

// NewEnvKeyEncrypter reads the env var, decodes the key, and returns an
// Encrypter. Errors when the env var is unset, not base64, or doesn't decode
// to 32 bytes.
func NewEnvKeyEncrypter(envVar string) (*EnvKeyEncrypter, error) {
	val := os.Getenv(envVar)
	if val == "" {
		return nil, fmt.Errorf("env-key encrypter: %s is not set", envVar)
	}
	key, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		return nil, fmt.Errorf("env-key encrypter: %s is not valid base64: %w", envVar, err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf(
			"env-key encrypter: %s decodes to %d bytes, want 32", envVar, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &EnvKeyEncrypter{aead: aead}, nil
}

// Encrypt seals plaintext with a fresh random nonce. Output bytes are
// `nonce || ciphertext+tag`.
func (e *EnvKeyEncrypter) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return e.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens a value produced by Encrypt. Errors on tampered or
// truncated bytes.
func (e *EnvKeyEncrypter) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < e.aead.NonceSize() {
		return nil, errors.New("env-key encrypter: ciphertext shorter than nonce")
	}
	nonce, payload := ciphertext[:e.aead.NonceSize()], ciphertext[e.aead.NonceSize():]
	return e.aead.Open(nil, nonce, payload, nil)
}
