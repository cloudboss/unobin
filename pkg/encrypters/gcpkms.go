package encrypters

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"

	"github.com/cloudboss/unobin/pkg/gcpcfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

var _ sdkencrypt.Encrypter = (*GCPKMS)(nil)

type GCPKMSConfig struct {
	KeyID string
	GCP   *gcpcfg.Configuration
}

func (c *GCPKMSConfig) Validate() error {
	if c.KeyID == "" {
		return fmt.Errorf("gcp-kms encrypter: %s is required", sdkencrypt.ConfigKeyID)
	}
	if c.GCP != nil {
		if err := c.GCP.Validate(); err != nil {
			return fmt.Errorf("gcp-kms encrypter: %w", err)
		}
	}
	return nil
}

type gcpKMSClient interface {
	encryptDataKey(ctx context.Context, keyID string, plaintext []byte) ([]byte, error)
	decryptDataKey(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error)
}

type GCPKMS struct {
	client gcpKMSClient
	keyID  string
	config map[string]any

	mu        sync.Mutex
	sealer    cipher.AEAD
	wrapped   []byte
	unwrapped map[string]cipher.AEAD
}

func NewGCPKMS(client gcpKMSClient, keyID string, config map[string]any) (*GCPKMS, error) {
	if client == nil {
		return nil, errors.New("gcp-kms encrypter: client is required")
	}
	if keyID == "" {
		return nil, fmt.Errorf("gcp-kms encrypter: %s is required", sdkencrypt.ConfigKeyID)
	}
	return &GCPKMS{
		client:    client,
		keyID:     keyID,
		config:    config,
		unwrapped: map[string]cipher.AEAD{},
	}, nil
}

func (k *GCPKMS) Describe() sdkencrypt.Description {
	config := maps.Clone(k.config)
	if config == nil {
		config = map[string]any{}
	}
	config[sdkencrypt.ConfigKeyID] = k.keyID
	return sdkencrypt.Description{KeySource: GCPKMSName, Config: config}
}

const gcpKMSSealedVersion = 1

type gcpKMSSealed struct {
	Version      int    `json:"version"`
	EncryptedKey []byte `json:"encrypted-key"`
	Payload      []byte `json:"payload"`
}

func (k *GCPKMS) Encrypt(plaintext []byte) ([]byte, error) {
	aead, wrapped, err := k.sealKey()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	blob := gcpKMSSealed{
		Version:      gcpKMSSealedVersion,
		EncryptedKey: wrapped,
		Payload:      aead.Seal(nonce, nonce, plaintext, nil),
	}
	return json.Marshal(blob)
}

func (k *GCPKMS) sealKey() (cipher.AEAD, []byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.sealer != nil {
		return k.sealer, k.wrapped, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, nil, err
	}
	defer clear(key)
	wrapped, err := k.client.encryptDataKey(context.Background(), k.keyID, key)
	if err != nil {
		return nil, nil, fmt.Errorf("gcp-kms encrypter: encrypt data key: %w", err)
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, nil, err
	}
	k.sealer = aead
	k.wrapped = append([]byte(nil), wrapped...)
	k.unwrapped[string(k.wrapped)] = aead
	return k.sealer, k.wrapped, nil
}

func (k *GCPKMS) Decrypt(ciphertext []byte) ([]byte, error) {
	var blob gcpKMSSealed
	if err := json.Unmarshal(ciphertext, &blob); err != nil {
		return nil, fmt.Errorf("gcp-kms encrypter: %w", err)
	}
	if blob.Version != gcpKMSSealedVersion {
		return nil, fmt.Errorf(
			"gcp-kms encrypter: unsupported version %d (this build expects %d)",
			blob.Version, gcpKMSSealedVersion)
	}
	aead, err := k.openKey(blob.EncryptedKey)
	if err != nil {
		return nil, err
	}
	if len(blob.Payload) < aead.NonceSize() {
		return nil, errors.New("gcp-kms encrypter: payload shorter than nonce")
	}
	nonce, payload := blob.Payload[:aead.NonceSize()], blob.Payload[aead.NonceSize():]
	opened, err := aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return nil, fmt.Errorf("gcp-kms encrypter: %w", err)
	}
	return opened, nil
}

func (k *GCPKMS) openKey(wrapped []byte) (cipher.AEAD, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if aead, ok := k.unwrapped[string(wrapped)]; ok {
		return aead, nil
	}
	key, err := k.client.decryptDataKey(context.Background(), k.keyID, wrapped)
	if err != nil {
		return nil, fmt.Errorf("gcp-kms encrypter: decrypt data key: %w", err)
	}
	defer clear(key)
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	k.unwrapped[string(wrapped)] = aead
	return aead, nil
}
