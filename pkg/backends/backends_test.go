package backends

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackendsRegistersLocal(t *testing.T) {
	bt, ok := Backends()["local"]
	require.True(t, ok, "expected a local backend")
	require.NotNil(t, bt.Configuration, "local takes a path configuration")
	assert.Equal(t, "local", bt.Name)
}

func TestEncryptersRegistersEnvKey(t *testing.T) {
	et, ok := Encrypters()["env-key"]
	require.True(t, ok, "expected an env-key encrypter")
	require.NotNil(t, et.Configuration)
	assert.Equal(t, "env-key", et.Name)
}

func TestEncryptersRegistersNoop(t *testing.T) {
	et, ok := Encrypters()["noop"]
	require.True(t, ok, "expected a noop encrypter")
	require.Nil(t, et.Configuration, "noop takes no configuration")

	enc, err := et.New(nil)
	require.NoError(t, err)
	ciphertext, err := enc.Encrypt([]byte("secret"))
	require.NoError(t, err)
	assert.Equal(t, []byte("secret"), ciphertext, "noop leaves plaintext unchanged")
}
