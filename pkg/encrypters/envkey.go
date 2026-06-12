package encrypters

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

// EnvKey uses AES-256-GCM with a 32 byte symmetric key read from a
// named environment variable. The env value must be the base64-encoded
// key.
type EnvKey struct {
	envVar string
	aead   cipher.AEAD
}

// NewEnvKey reads the env var, decodes the key, and returns an
// EnvKey encrypter. Errors when the env var is unset, not base64, or
// does not decode to 32 bytes.
func NewEnvKey(envVar string) (*EnvKey, error) {
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
	return &EnvKey{envVar: envVar, aead: aead}, nil
}

// Describe names the env-key source and the env var the key is read
// from.
func (e *EnvKey) Describe() sdkencrypt.Description {
	return sdkencrypt.Description{
		KeySource: "env-key",
		Config:    map[string]any{"env-var": e.envVar},
	}
}

// Encrypt seals plaintext with a fresh random nonce. Output bytes are
// `nonce || ciphertext+tag`.
func (e *EnvKey) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return e.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens a value produced by Encrypt. Errors on tampered or
// truncated bytes.
func (e *EnvKey) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < e.aead.NonceSize() {
		return nil, errors.New("env-key encrypter: ciphertext shorter than nonce")
	}
	nonce, payload := ciphertext[:e.aead.NonceSize()], ciphertext[e.aead.NonceSize():]
	return e.aead.Open(nil, nonce, payload, nil)
}
