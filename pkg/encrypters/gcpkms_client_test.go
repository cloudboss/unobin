package encrypters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/cloudkms/v1"
	"google.golang.org/api/option"
)

func newTestGCPKMSRESTClient(t *testing.T, handler http.HandlerFunc) gcpKMSClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	service, err := cloudkms.NewService(
		context.Background(),
		option.WithEndpoint(server.URL+"/"),
		option.WithoutAuthentication(),
	)
	require.NoError(t, err)
	return newGCPKMSRESTClient(service)
}

func TestGCPKMSRESTEncryptSendsBase64PlaintextAndCRC32C(t *testing.T) {
	plaintext := []byte("data-key")
	wrapped := []byte("wrapped-key")
	client := newTestGCPKMSRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/"+testGCPKMSKeyID+":encrypt", r.URL.Path)
		var req map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, base64.StdEncoding.EncodeToString(plaintext), req["plaintext"])
		assert.Equal(t, fmt.Sprint(testCRC32C(plaintext)), req["plaintextCrc32c"])
		writeKMSJSON(t, w, map[string]any{
			"ciphertext":              base64.StdEncoding.EncodeToString(wrapped),
			"ciphertextCrc32c":        fmt.Sprint(testCRC32C(wrapped)),
			"verifiedPlaintextCrc32c": true,
		})
	})

	got, err := client.encryptDataKey(context.Background(), testGCPKMSKeyID, plaintext)
	require.NoError(t, err)
	assert.Equal(t, wrapped, got)
}

func TestGCPKMSRESTEncryptVerifiesResponseCRC32C(t *testing.T) {
	wrapped := []byte("wrapped-key")
	client := newTestGCPKMSRESTClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeKMSJSON(t, w, map[string]any{
			"ciphertext":              base64.StdEncoding.EncodeToString(wrapped),
			"ciphertextCrc32c":        fmt.Sprint(testCRC32C(wrapped)),
			"verifiedPlaintextCrc32c": true,
		})
	})
	got, err := client.encryptDataKey(context.Background(), testGCPKMSKeyID, []byte("data-key"))
	require.NoError(t, err)
	assert.Equal(t, wrapped, got)

	bad := newTestGCPKMSRESTClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeKMSJSON(t, w, map[string]any{
			"ciphertext":              base64.StdEncoding.EncodeToString(wrapped),
			"ciphertextCrc32c":        "1",
			"verifiedPlaintextCrc32c": true,
		})
	})
	_, err = bad.encryptDataKey(context.Background(), testGCPKMSKeyID, []byte("data-key"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ciphertext CRC32C")
}

func TestGCPKMSRESTEncryptRequiresVerifiedPlaintextCRC32C(t *testing.T) {
	wrapped := []byte("wrapped-key")
	client := newTestGCPKMSRESTClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeKMSJSON(t, w, map[string]any{
			"ciphertext":              base64.StdEncoding.EncodeToString(wrapped),
			"ciphertextCrc32c":        fmt.Sprint(testCRC32C(wrapped)),
			"verifiedPlaintextCrc32c": false,
		})
	})

	_, err := client.encryptDataKey(context.Background(), testGCPKMSKeyID, []byte("data-key"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verified plaintext CRC32C")
}

func TestGCPKMSRESTDecryptSendsCiphertextCRC32C(t *testing.T) {
	wrapped := []byte("wrapped-key")
	plaintext := []byte("data-key")
	client := newTestGCPKMSRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/"+testGCPKMSKeyID+":decrypt", r.URL.Path)
		var req map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, base64.StdEncoding.EncodeToString(wrapped), req["ciphertext"])
		assert.Equal(t, fmt.Sprint(testCRC32C(wrapped)), req["ciphertextCrc32c"])
		writeKMSJSON(t, w, map[string]any{
			"plaintext":       base64.StdEncoding.EncodeToString(plaintext),
			"plaintextCrc32c": fmt.Sprint(testCRC32C(plaintext)),
		})
	})

	got, err := client.decryptDataKey(context.Background(), testGCPKMSKeyID, wrapped)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)
}

func TestGCPKMSRESTDecryptVerifiesPlaintextCRC32C(t *testing.T) {
	plaintext := []byte("data-key")
	client := newTestGCPKMSRESTClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeKMSJSON(t, w, map[string]any{
			"plaintext":       base64.StdEncoding.EncodeToString(plaintext),
			"plaintextCrc32c": fmt.Sprint(testCRC32C(plaintext)),
		})
	})
	got, err := client.decryptDataKey(context.Background(), testGCPKMSKeyID, []byte("wrapped"))
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)

	bad := newTestGCPKMSRESTClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeKMSJSON(t, w, map[string]any{
			"plaintext":       base64.StdEncoding.EncodeToString(plaintext),
			"plaintextCrc32c": "1",
		})
	})
	_, err = bad.decryptDataKey(context.Background(), testGCPKMSKeyID, []byte("wrapped"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plaintext CRC32C")
}

func TestGCPKMSRESTPropagatesAPIError(t *testing.T) {
	client := newTestGCPKMSRESTClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeKMSJSON(t, w, map[string]any{
			"error": map[string]any{
				"code":    http.StatusServiceUnavailable,
				"message": "unavailable",
				"status":  "UNAVAILABLE",
			},
		})
	})

	_, err := client.encryptDataKey(context.Background(), testGCPKMSKeyID, []byte("data-key"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encrypt data key")
	assert.Contains(t, err.Error(), "unavailable")
}

func testCRC32C(data []byte) uint32 {
	return crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli))
}

func writeKMSJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(body))
}
