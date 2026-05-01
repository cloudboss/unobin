package state

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func setKey(t *testing.T, envVar string) []byte {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	t.Setenv(envVar, base64.StdEncoding.EncodeToString(key))
	return key
}

func TestNoopEncrypterPassesThrough(t *testing.T) {
	e := NoopEncrypter{}
	ct, err := e.Encrypt([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), ct)
	pt, err := e.Decrypt(ct)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), pt)
}

func TestEnvKeyEncrypterRoundTrip(t *testing.T) {
	setKey(t, "UB_TEST_KEY")
	e, err := NewEnvKeyEncrypter("UB_TEST_KEY")
	require.NoError(t, err)

	plaintext := []byte("the quick brown fox jumps over the lazy dog")
	ct, err := e.Encrypt(plaintext)
	require.NoError(t, err)
	require.NotEqual(t, plaintext, ct)

	pt, err := e.Decrypt(ct)
	require.NoError(t, err)
	require.Equal(t, plaintext, pt)
}

func TestEnvKeyEncrypterUsesFreshNonce(t *testing.T) {
	setKey(t, "UB_TEST_KEY")
	e, err := NewEnvKeyEncrypter("UB_TEST_KEY")
	require.NoError(t, err)

	a, err := e.Encrypt([]byte("same plaintext"))
	require.NoError(t, err)
	b, err := e.Encrypt([]byte("same plaintext"))
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

func TestEnvKeyEncrypterRejectsTamper(t *testing.T) {
	setKey(t, "UB_TEST_KEY")
	e, err := NewEnvKeyEncrypter("UB_TEST_KEY")
	require.NoError(t, err)

	ct, err := e.Encrypt([]byte("payload"))
	require.NoError(t, err)
	ct[len(ct)-1] ^= 0x01

	_, err = e.Decrypt(ct)
	require.Error(t, err)
}

func TestEnvKeyEncrypterRejectsShortCiphertext(t *testing.T) {
	setKey(t, "UB_TEST_KEY")
	e, err := NewEnvKeyEncrypter("UB_TEST_KEY")
	require.NoError(t, err)

	_, err = e.Decrypt([]byte("nope"))
	require.Error(t, err)
}

func TestEnvKeyEncrypterRejectsMissingEnv(t *testing.T) {
	t.Setenv("UB_TEST_KEY", "")
	_, err := NewEnvKeyEncrypter("UB_TEST_KEY")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not set")
}

func TestEnvKeyEncrypterRejectsBadBase64(t *testing.T) {
	t.Setenv("UB_TEST_KEY", "!!!not-base64!!!")
	_, err := NewEnvKeyEncrypter("UB_TEST_KEY")
	require.Error(t, err)
	require.Contains(t, err.Error(), "base64")
}

func TestEnvKeyEncrypterRejectsWrongKeyLength(t *testing.T) {
	short := make([]byte, 16)
	_, _ = rand.Read(short)
	t.Setenv("UB_TEST_KEY", base64.StdEncoding.EncodeToString(short))
	_, err := NewEnvKeyEncrypter("UB_TEST_KEY")
	require.Error(t, err)
	require.Contains(t, err.Error(), "32")
}

func TestEnvKeyEncrypterDifferentKeysDontOpen(t *testing.T) {
	setKey(t, "UB_TEST_KEY_A")
	setKey(t, "UB_TEST_KEY_B")
	a, err := NewEnvKeyEncrypter("UB_TEST_KEY_A")
	require.NoError(t, err)
	b, err := NewEnvKeyEncrypter("UB_TEST_KEY_B")
	require.NoError(t, err)

	ct, err := a.Encrypt([]byte("secret"))
	require.NoError(t, err)
	_, err = b.Decrypt(ct)
	require.Error(t, err)
}
