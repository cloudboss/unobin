package backends

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
)

func TestBackendsRegistersLocal(t *testing.T) {
	bt, ok := Backends()["local"]
	require.True(t, ok, "expected a local backend")
	require.NotNil(t, bt.Configuration, "local takes a path configuration")
	assert.Equal(t, "local", bt.Name)
}

func TestBackendsRegistersS3(t *testing.T) {
	bt, ok := Backends()["s3"]
	require.True(t, ok, "expected an s3 backend")
	require.NotNil(t, bt.Configuration)
	assert.Equal(t, "s3", bt.Name)
}

// The decoder maps Go fields to UB keys with PascalToKebab and no tag
// override, so every exported field must kebab to exactly the
// operator-facing name.
func TestS3BackendConfigKebabNames(t *testing.T) {
	expected := []string{"bucket", "prefix", "kms-key-id", "use-path-style", "aws"}
	var got []string
	for f := range reflect.TypeFor[S3BackendConfig]().Fields() {
		got = append(got, lang.PascalToKebab(f.Name))
	}
	assert.Equal(t, expected, got)
}

func TestEncryptersRegistersKMS(t *testing.T) {
	et, ok := Encrypters()["kms"]
	require.True(t, ok, "expected a kms encrypter")
	require.NotNil(t, et.Configuration)
	assert.Equal(t, "kms", et.Name)
}

func TestKMSConfigKebabNames(t *testing.T) {
	expected := []string{"key-id", "aws"}
	var got []string
	for f := range reflect.TypeFor[KMSConfig]().Fields() {
		got = append(got, lang.PascalToKebab(f.Name))
	}
	assert.Equal(t, expected, got)
}

func TestNewKMSEncrypterRequiresKeyID(t *testing.T) {
	_, err := newKMSEncrypter(&KMSConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key-id is required")
}

func TestNewKMSEncrypterRejectsWrongConfigType(t *testing.T) {
	_, err := newKMSEncrypter(&EnvKeyConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing or wrong configuration")
}

func TestNewS3BackendRequiresBucket(t *testing.T) {
	_, err := newS3Backend(&S3BackendConfig{}, "factory", "stack", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bucket is required")
}

func TestNewS3BackendRejectsWrongConfigType(t *testing.T) {
	_, err := newS3Backend(&LocalBackendConfig{}, "factory", "stack", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing or wrong configuration")
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
