package backends

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/encrypters"
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

func TestNewLocalBackendAcceptsPlainConfig(t *testing.T) {
	backend, err := newLocalBackend(
		&LocalBackendConfig{Path: t.TempDir()}, "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	assert.NotNil(t, backend)
}

func TestNewLocalBackendRequiresPath(t *testing.T) {
	_, err := newLocalBackend(&LocalBackendConfig{}, "factory", "stack", encrypters.Noop{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
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
