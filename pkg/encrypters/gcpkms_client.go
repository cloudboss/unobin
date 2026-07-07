package encrypters

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash/crc32"

	"google.golang.org/api/cloudkms/v1"
)

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

type gcpKMSRESTClient struct {
	service *cloudkms.Service
}

func newGCPKMSRESTClient(service *cloudkms.Service) gcpKMSClient {
	return &gcpKMSRESTClient{service: service}
}

func (c *gcpKMSRESTClient) encryptDataKey(
	ctx context.Context,
	keyID string,
	plaintext []byte,
) ([]byte, error) {
	resp, err := c.service.Projects.Locations.KeyRings.CryptoKeys.Encrypt(
		keyID,
		&cloudkms.EncryptRequest{
			Plaintext:       base64.StdEncoding.EncodeToString(plaintext),
			PlaintextCrc32c: int64(crc32c(plaintext)),
		},
	).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("gcp-kms client: encrypt data key: %w", err)
	}
	if !resp.VerifiedPlaintextCrc32c {
		return nil, fmt.Errorf("gcp-kms client: encrypt data key: verified plaintext CRC32C false")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(resp.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("gcp-kms client: encrypt data key: decode ciphertext: %w", err)
	}
	if got, want := crc32c(ciphertext), uint32(resp.CiphertextCrc32c); got != want {
		return nil, fmt.Errorf(
			"gcp-kms client: encrypt data key: ciphertext CRC32C %d, expected %d",
			got, want)
	}
	return ciphertext, nil
}

func (c *gcpKMSRESTClient) decryptDataKey(
	ctx context.Context,
	keyID string,
	ciphertext []byte,
) ([]byte, error) {
	resp, err := c.service.Projects.Locations.KeyRings.CryptoKeys.Decrypt(
		keyID,
		&cloudkms.DecryptRequest{
			Ciphertext:       base64.StdEncoding.EncodeToString(ciphertext),
			CiphertextCrc32c: int64(crc32c(ciphertext)),
		},
	).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("gcp-kms client: decrypt data key: %w", err)
	}
	plaintext, err := base64.StdEncoding.DecodeString(resp.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("gcp-kms client: decrypt data key: decode plaintext: %w", err)
	}
	if got, want := crc32c(plaintext), uint32(resp.PlaintextCrc32c); got != want {
		return nil, fmt.Errorf(
			"gcp-kms client: decrypt data key: plaintext CRC32C %d, expected %d",
			got, want)
	}
	return plaintext, nil
}

func crc32c(data []byte) uint32 {
	return crc32.Checksum(data, crc32cTable)
}
