package encrypters

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
)

// fakeKeyARN is the KeyId the fake returns from GenerateDataKey. The
// real service returns the key ARN there no matter how the request
// named the key.
const fakeKeyARN = "arn:aws:kms:us-east-1:000000000000:key/00000000-0000-0000-0000-000000000000"

// fakeKMS is an in-process KMS speaking just enough of the awsjson
// protocol for the encrypter: GenerateDataKey hands out a fresh
// 32-byte key and remembers it under an opaque wrapped blob, and
// Decrypt unwraps only blobs this instance produced, so a foreign or
// tampered blob fails the way a wrong KMS key does.
type fakeKMS struct {
	mu   sync.Mutex
	keys map[string][]byte
	gens []string
	decs int
}

func newFakeKMS() *fakeKMS {
	return &fakeKMS{keys: map[string][]byte{}}
}

func (f *fakeKMS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Header.Get("X-Amz-Target") {
	case "TrentService.GenerateDataKey":
		f.generateDataKey(w, r)
	case "TrentService.Decrypt":
		f.decrypt(w, r)
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func (f *fakeKMS) generateDataKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		KeyId   string
		KeySpec string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	plaintext := make([]byte, 32)
	if _, err := rand.Read(plaintext); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	wrapped := make([]byte, 16)
	if _, err := rand.Read(wrapped); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	f.mu.Lock()
	f.keys[hex.EncodeToString(wrapped)] = plaintext
	f.gens = append(f.gens, req.KeyId)
	f.mu.Unlock()
	writeJSON(w, map[string]any{
		"KeyId":          fakeKeyARN,
		"KeySpec":        req.KeySpec,
		"CiphertextBlob": wrapped,
		"Plaintext":      plaintext,
	})
}

func (f *fakeKMS) decrypt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CiphertextBlob string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	wrapped, err := base64.StdEncoding.DecodeString(req.CiphertextBlob)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.decs++
	plaintext, ok := f.keys[hex.EncodeToString(wrapped)]
	f.mu.Unlock()
	if !ok {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"__type":"InvalidCiphertextException"}`))
		return
	}
	writeJSON(w, map[string]any{
		"KeyId":     "fake-key",
		"Plaintext": plaintext,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	body, _ := json.Marshal(v)
	_, _ = w.Write(body)
}

// generated returns the KeyId of every GenerateDataKey call.
func (f *fakeKMS) generated() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.gens))
	copy(out, f.gens)
	return out
}

// decryptCalls returns how many Decrypt requests the fake has served.
func (f *fakeKMS) decryptCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.decs
}
