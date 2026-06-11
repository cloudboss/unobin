// Package kmsencrypt seals state and plan bytes with envelope
// encryption through AWS KMS. Each Encrypt call has KMS generate a
// fresh 256-bit data key under the configured key, seals the payload
// locally with AES-256-GCM, and stores the KMS-wrapped data key
// beside the sealed bytes. Decrypt has KMS unwrap the stored data key
// and opens the payload locally, so data keys are the only thing that
// crosses the wire, never the payload.
package kmsencrypt

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"

	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

var _ sdkencrypt.Encrypter = (*KMS)(nil)

// KMS seals and unseals bytes with data keys wrapped by one KMS key,
// named by id, ARN, or alias.
type KMS struct {
	client *kms.Client
	keyID  string
}

// New returns a KMS encrypter using client and the given key.
func New(client *kms.Client, keyID string) (*KMS, error) {
	if client == nil {
		return nil, errors.New("kms encrypter: client is required")
	}
	if keyID == "" {
		return nil, errors.New("kms encrypter: key-id is required")
	}
	return &KMS{client: client, keyID: keyID}, nil
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

// Encrypt seals plaintext under a fresh KMS data key.
func (k *KMS) Encrypt(plaintext []byte) ([]byte, error) {
	out, err := k.client.GenerateDataKey(context.Background(), &kms.GenerateDataKeyInput{
		KeyId:   aws.String(k.keyID),
		KeySpec: kmstypes.DataKeySpecAes256,
	})
	if err != nil {
		return nil, fmt.Errorf("kms encrypter: generate data key: %w", err)
	}
	defer clear(out.Plaintext)
	aead, err := newAEAD(out.Plaintext)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	blob := sealed{
		Version:      sealedVersion,
		EncryptedKey: out.CiphertextBlob,
		Payload:      aead.Seal(nonce, nonce, plaintext, nil),
	}
	return json.Marshal(blob)
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
	out, err := k.client.Decrypt(context.Background(), &kms.DecryptInput{
		CiphertextBlob: blob.EncryptedKey,
	})
	if err != nil {
		return nil, fmt.Errorf("kms encrypter: decrypt data key: %w", err)
	}
	defer clear(out.Plaintext)
	aead, err := newAEAD(out.Plaintext)
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

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
