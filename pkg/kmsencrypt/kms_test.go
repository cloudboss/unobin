package kmsencrypt

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// testClient builds a real KMS client against the fake server,
// through awscfg the same way the encrypter constructor builds it.
func testClient(t *testing.T, url string) *kms.Client {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	awsCfg, err := awscfg.Load(context.Background(), &awscfg.Configuration{
		Region:      &cfg.String{Value: "us-east-1"},
		EndpointURL: &cfg.String{Value: url},
	})
	require.NoError(t, err)
	return kms.NewFromConfig(awsCfg)
}

func testEncrypter(t *testing.T) (*KMS, *fakeKMS) {
	t.Helper()
	fake := newFakeKMS()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	enc, err := New(testClient(t, srv.URL), "alias/unobin-state")
	require.NoError(t, err)
	return enc, fake
}

func TestNewRequiredArguments(t *testing.T) {
	_, err := New(nil, "alias/x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client is required")

	_, err = New(&kms.Client{}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key-id is required")
}

func TestEncryptDecrypt(t *testing.T) {
	enc, _ := testEncrypter(t)
	plaintext := []byte("state snapshot bytes")
	sealed, err := enc.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotContains(t, string(sealed), "state snapshot bytes")

	opened, err := enc.Decrypt(sealed)
	require.NoError(t, err)
	assert.Equal(t, plaintext, opened)
}

func TestEncryptUsesConfiguredKey(t *testing.T) {
	enc, fake := testEncrypter(t)
	_, err := enc.Encrypt([]byte("x"))
	require.NoError(t, err)
	assert.Equal(t, []string{"alias/unobin-state"}, fake.generated())
}

func TestEncryptFreshDataKeyPerCall(t *testing.T) {
	enc, _ := testEncrypter(t)
	first, err := enc.Encrypt([]byte("x"))
	require.NoError(t, err)
	second, err := enc.Encrypt([]byte("x"))
	require.NoError(t, err)

	var a, b struct {
		EncryptedKey []byte `json:"encrypted-key"`
	}
	require.NoError(t, json.Unmarshal(first, &a))
	require.NoError(t, json.Unmarshal(second, &b))
	assert.NotEqual(t, a.EncryptedKey, b.EncryptedKey)
}

func TestDecryptTamperedPayload(t *testing.T) {
	enc, _ := testEncrypter(t)
	sealed, err := enc.Encrypt([]byte("payload"))
	require.NoError(t, err)

	var blob struct {
		Version      int    `json:"version"`
		EncryptedKey []byte `json:"encrypted-key"`
		Payload      []byte `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(sealed, &blob))
	blob.Payload[len(blob.Payload)-1] ^= 0xff
	tampered, err := json.Marshal(blob)
	require.NoError(t, err)

	_, err = enc.Decrypt(tampered)
	require.Error(t, err)
}

func TestDecryptForeignDataKey(t *testing.T) {
	enc, _ := testEncrypter(t)
	sealed, err := enc.Encrypt([]byte("payload"))
	require.NoError(t, err)

	other, _ := testEncrypter(t)
	_, err = other.Decrypt(sealed)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt data key")
}

func TestDecryptGarbage(t *testing.T) {
	enc, _ := testEncrypter(t)
	_, err := enc.Decrypt([]byte("not json"))
	require.Error(t, err)
}

func TestDecryptUnsupportedVersion(t *testing.T) {
	enc, _ := testEncrypter(t)
	sealed, err := enc.Encrypt([]byte("payload"))
	require.NoError(t, err)

	var blob map[string]any
	require.NoError(t, json.Unmarshal(sealed, &blob))
	blob["version"] = 99
	bumped, err := json.Marshal(blob)
	require.NoError(t, err)

	_, err = enc.Decrypt(bumped)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported version")
}
