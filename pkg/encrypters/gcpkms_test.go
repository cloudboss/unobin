package encrypters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testGCPKMSKeyID = "projects/p/locations/us/keyRings/r/cryptoKeys/k"

type fakeGCPKMSClient struct {
	mu       sync.Mutex
	keys     map[string][]byte
	encrypts int
	decrypts int
}

func newFakeGCPKMSClient() *fakeGCPKMSClient {
	return &fakeGCPKMSClient{keys: map[string][]byte{}}
}

func (f *fakeGCPKMSClient) encryptDataKey(
	_ context.Context,
	_ string,
	plaintext []byte,
) ([]byte, error) {
	wrapped := make([]byte, 16)
	if _, err := rand.Read(wrapped); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys[hex.EncodeToString(wrapped)] = append([]byte(nil), plaintext...)
	f.encrypts++
	return wrapped, nil
}

func (f *fakeGCPKMSClient) decryptDataKey(
	_ context.Context,
	_ string,
	ciphertext []byte,
) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decrypts++
	plaintext, ok := f.keys[hex.EncodeToString(ciphertext)]
	if !ok {
		return nil, errors.New("unknown wrapped key")
	}
	return append([]byte(nil), plaintext...), nil
}

func (f *fakeGCPKMSClient) encryptCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.encrypts
}

func (f *fakeGCPKMSClient) decryptCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.decrypts
}

func testGCPKMSEncrypter(t *testing.T) (*GCPKMS, *fakeGCPKMSClient) {
	t.Helper()
	fake := newFakeGCPKMSClient()
	enc, err := NewGCPKMS(fake, testGCPKMSKeyID, nil)
	require.NoError(t, err)
	return enc, fake
}

func TestNewGCPKMSRequiresClientAndKeyID(t *testing.T) {
	_, err := NewGCPKMS(nil, testGCPKMSKeyID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client is required")

	_, err = NewGCPKMS(newFakeGCPKMSClient(), "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key-id")
}

func TestGCPKMSEncryptDecrypt(t *testing.T) {
	enc, _ := testGCPKMSEncrypter(t)
	plaintext := []byte("state snapshot bytes")
	sealed, err := enc.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotContains(t, string(sealed), "state snapshot bytes")

	opened, err := enc.Decrypt(sealed)
	require.NoError(t, err)
	assert.Equal(t, plaintext, opened)
}

func TestGCPKMSDescribe(t *testing.T) {
	config := map[string]any{
		"key-id": testGCPKMSKeyID,
		"gcp":    map[string]any{"project": "test-project"},
	}
	enc, err := NewGCPKMS(newFakeGCPKMSClient(), testGCPKMSKeyID, config)
	require.NoError(t, err)

	desc := enc.Describe()
	assert.Equal(t, "gcp-kms", desc.KeySource)
	assert.Equal(t, testGCPKMSKeyID, desc.Config["key-id"])
	assert.Equal(t, map[string]any{"project": "test-project"}, desc.Config["gcp"])

	_, err = enc.Encrypt([]byte("payload"))
	require.NoError(t, err)
	desc = enc.Describe()
	assert.Equal(t, testGCPKMSKeyID, desc.Config["key-id"])
}

func TestGCPKMSUsesOneWrappedKeyPerEncrypter(t *testing.T) {
	enc, fake := testGCPKMSEncrypter(t)
	first, err := enc.Encrypt([]byte("one"))
	require.NoError(t, err)
	second, err := enc.Encrypt([]byte("two"))
	require.NoError(t, err)

	assert.Equal(t, 1, fake.encryptCalls())
	assert.NotEqual(t, first, second)

	var a, b struct {
		EncryptedKey []byte `json:"encrypted-key"`
	}
	require.NoError(t, json.Unmarshal(first, &a))
	require.NoError(t, json.Unmarshal(second, &b))
	assert.Equal(t, a.EncryptedKey, b.EncryptedKey)
}

func TestGCPKMSMemoizesDecryptedKeys(t *testing.T) {
	writer, fake := testGCPKMSEncrypter(t)
	first, err := writer.Encrypt([]byte("one"))
	require.NoError(t, err)
	second, err := writer.Encrypt([]byte("two"))
	require.NoError(t, err)

	reader, err := NewGCPKMS(fake, testGCPKMSKeyID, nil)
	require.NoError(t, err)
	_, err = reader.Decrypt(first)
	require.NoError(t, err)
	_, err = reader.Decrypt(second)
	require.NoError(t, err)
	assert.Equal(t, 1, fake.decryptCalls())
}

func TestGCPKMSRejectsTamper(t *testing.T) {
	enc, _ := testGCPKMSEncrypter(t)
	sealed, err := enc.Encrypt([]byte("payload"))
	require.NoError(t, err)

	var blob gcpKMSSealed
	require.NoError(t, json.Unmarshal(sealed, &blob))
	blob.Payload[len(blob.Payload)-1] ^= 0xff
	tampered, err := json.Marshal(blob)
	require.NoError(t, err)

	_, err = enc.Decrypt(tampered)
	require.Error(t, err)
}

func TestGCPKMSRejectsForeignWrappedKey(t *testing.T) {
	enc, _ := testGCPKMSEncrypter(t)
	sealed, err := enc.Encrypt([]byte("payload"))
	require.NoError(t, err)

	other, err := NewGCPKMS(newFakeGCPKMSClient(), testGCPKMSKeyID, nil)
	require.NoError(t, err)
	_, err = other.Decrypt(sealed)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt data key")
}

func TestGCPKMSRejectsUnsupportedVersion(t *testing.T) {
	enc, _ := testGCPKMSEncrypter(t)
	sealed, err := enc.Encrypt([]byte("payload"))
	require.NoError(t, err)

	var blob gcpKMSSealed
	require.NoError(t, json.Unmarshal(sealed, &blob))
	blob.Version = 99
	bumped, err := json.Marshal(blob)
	require.NoError(t, err)

	_, err = enc.Decrypt(bumped)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported version")
}

func TestGCPKMSRejectsShortPayload(t *testing.T) {
	enc, _ := testGCPKMSEncrypter(t)
	sealed, err := enc.Encrypt([]byte("payload"))
	require.NoError(t, err)

	var blob gcpKMSSealed
	require.NoError(t, json.Unmarshal(sealed, &blob))
	blob.Payload = []byte{1, 2}
	shortPayload, err := json.Marshal(blob)
	require.NoError(t, err)

	_, err = enc.Decrypt(shortPayload)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payload shorter than nonce")
}
