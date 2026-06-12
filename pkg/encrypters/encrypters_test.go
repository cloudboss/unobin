package encrypters

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

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

	enc, err := et.New(nil, nil)
	require.NoError(t, err)
	ciphertext, err := enc.Encrypt([]byte("secret"))
	require.NoError(t, err)
	assert.Equal(t, []byte("secret"), ciphertext, "noop leaves plaintext unchanged")
}

func TestEncryptersRegistersKMS(t *testing.T) {
	et, ok := Encrypters()["kms"]
	require.True(t, ok, "expected a kms encrypter")
	require.NotNil(t, et.Configuration)
	assert.Equal(t, "kms", et.Name)
}

// The decoder maps Go fields to UB keys with PascalToKebab and no tag
// override, so every exported field must kebab to exactly the
// operator-facing name.
func TestKMSConfigKebabNames(t *testing.T) {
	assert.Equal(t, []string{"key-id", "aws"}, kebabFieldNames[KMSConfig]())
}

func TestConfigKeyConstantsMatchSchemas(t *testing.T) {
	assert.Contains(t, kebabFieldNames[KMSConfig](), sdkencrypt.ConfigKeyID)
	assert.Contains(t, kebabFieldNames[EnvKeyConfig](), sdkencrypt.ConfigEnvVar)
}

func kebabFieldNames[T any]() []string {
	var names []string
	for f := range reflect.TypeFor[T]().Fields() {
		names = append(names, lang.PascalToKebab(f.Name))
	}
	return names
}

func TestNewKMSEncrypterRequiresKeyID(t *testing.T) {
	_, err := newKMSEncrypter(&KMSConfig{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key-id is required")
}

func TestNewKMSEncrypterRejectsWrongConfigType(t *testing.T) {
	_, err := newKMSEncrypter(&EnvKeyConfig{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing or wrong configuration")
}
