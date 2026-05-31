package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoreRegistersNoopEncrypter(t *testing.T) {
	et, ok := Library().Encrypters["noop"]
	require.True(t, ok, "core should register a noop encrypter")
	require.Nil(t, et.Configuration, "noop takes no configuration")

	enc, err := et.New(nil)
	require.NoError(t, err)

	ciphertext, err := enc.Encrypt([]byte("secret"))
	require.NoError(t, err)
	assert.Equal(t, []byte("secret"), ciphertext, "noop leaves plaintext unchanged")

	plaintext, err := enc.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, []byte("secret"), plaintext)
}
