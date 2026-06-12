package encrypters

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"

	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

var _ sdkencrypt.Encrypter = (*KMS)(nil)

// KMS seals and unseals bytes with envelope encryption through AWS
// KMS: payloads are sealed locally with AES-256-GCM under a 256-bit
// data key that KMS generates and wraps with one KMS key (named by
// id, ARN, or alias), and each blob stores its wrapped data key, so
// data keys are the only thing that crosses the wire, never the
// payload.
//
// An encrypter generates one data key on first use and seals every
// write with it. An encrypter lives for one command, so the key's
// exposure is bounded by the run, GCM with random nonces stays safe
// for far more messages than a run writes, and readers never depend
// on the reuse because every blob is self-describing. Unwrapped data
// keys are memoized by their wrapped bytes for the same reason in
// the other direction: a run re-reading blobs it wrote, or several
// blobs sealed under one data key, costs one KMS call at most.
type KMS struct {
	client *kms.Client
	keyID  string
	config map[string]any

	mu        sync.Mutex
	sealer    cipher.AEAD
	wrapped   []byte
	unwrapped map[string]cipher.AEAD
}

// NewKMS returns a KMS encrypter using client and the given key.
// config, which may be nil, is the operator's evaluated encryption
// block; Describe reports it so sealed files record how to decrypt.
func NewKMS(client *kms.Client, keyID string, config map[string]any) (*KMS, error) {
	if client == nil {
		return nil, errors.New("kms encrypter: client is required")
	}
	if keyID == "" {
		return nil, errors.New("kms encrypter: key-id is required")
	}
	return &KMS{
		client:    client,
		keyID:     keyID,
		config:    config,
		unwrapped: map[string]cipher.AEAD{},
	}, nil
}

// Describe names the kms key source and the operator configuration
// that selects the key.
func (k *KMS) Describe() sdkencrypt.Description {
	config := maps.Clone(k.config)
	if config == nil {
		config = map[string]any{}
	}
	config["key-id"] = k.keyID
	return sdkencrypt.Description{KeySource: "kms", Config: config}
}

const sealedVersion = 1

// sealed is the blob Encrypt produces. EncryptedKey is the
// KMS-wrapped data key; Payload is nonce || ciphertext+tag, the same
// framing the env-key encrypter uses.
type sealed struct {
	Version      int    `json:"version"`
	EncryptedKey []byte `json:"encrypted-key"`
	Payload      []byte `json:"payload"`
}

// Encrypt seals plaintext under the run's KMS data key, generating
// it on first use.
func (k *KMS) Encrypt(plaintext []byte) ([]byte, error) {
	aead, wrapped, err := k.sealKey()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	blob := sealed{
		Version:      sealedVersion,
		EncryptedKey: wrapped,
		Payload:      aead.Seal(nonce, nonce, plaintext, nil),
	}
	return json.Marshal(blob)
}

// sealKey returns the data key every Encrypt seals with, asking KMS
// to generate it the first time. The wrapped form is also memoized
// for Decrypt, so re-reading a blob this run wrote needs no KMS call.
func (k *KMS) sealKey() (cipher.AEAD, []byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.sealer != nil {
		return k.sealer, k.wrapped, nil
	}
	out, err := k.client.GenerateDataKey(context.Background(), &kms.GenerateDataKeyInput{
		KeyId:   aws.String(k.keyID),
		KeySpec: kmstypes.DataKeySpecAes256,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("kms encrypter: generate data key: %w", err)
	}
	defer clear(out.Plaintext)
	aead, err := newAEAD(out.Plaintext)
	if err != nil {
		return nil, nil, err
	}
	k.sealer = aead
	k.wrapped = out.CiphertextBlob
	k.unwrapped[string(out.CiphertextBlob)] = aead
	return k.sealer, k.wrapped, nil
}

// Decrypt opens a value produced by Encrypt. Errors on tampered or
// truncated bytes, and when KMS will not unwrap the stored data key.
func (k *KMS) Decrypt(ciphertext []byte) ([]byte, error) {
	var blob sealed
	if err := json.Unmarshal(ciphertext, &blob); err != nil {
		return nil, fmt.Errorf("kms encrypter: %w", err)
	}
	if blob.Version != sealedVersion {
		return nil, fmt.Errorf(
			"kms encrypter: unsupported version %d (this build expects %d)",
			blob.Version, sealedVersion)
	}
	aead, err := k.openKey(blob.EncryptedKey)
	if err != nil {
		return nil, err
	}
	if len(blob.Payload) < aead.NonceSize() {
		return nil, errors.New("kms encrypter: payload shorter than nonce")
	}
	nonce, payload := blob.Payload[:aead.NonceSize()], blob.Payload[aead.NonceSize():]
	opened, err := aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return nil, fmt.Errorf("kms encrypter: %w", err)
	}
	return opened, nil
}

// openKey returns the data key a blob was sealed with, asking KMS to
// unwrap it on first sight. The memo is keyed by the wrapped bytes
// and holds one entry per distinct data key seen, which a
// command-scoped process keeps to a handful.
func (k *KMS) openKey(wrapped []byte) (cipher.AEAD, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if aead, ok := k.unwrapped[string(wrapped)]; ok {
		return aead, nil
	}
	out, err := k.client.Decrypt(context.Background(), &kms.DecryptInput{
		CiphertextBlob: wrapped,
	})
	if err != nil {
		return nil, fmt.Errorf("kms encrypter: decrypt data key: %w", err)
	}
	defer clear(out.Plaintext)
	aead, err := newAEAD(out.Plaintext)
	if err != nil {
		return nil, err
	}
	k.unwrapped[string(wrapped)] = aead
	return aead, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
