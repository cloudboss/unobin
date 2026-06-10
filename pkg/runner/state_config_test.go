package runner

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/envencrypt"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseConfig parses an in-memory config.ub-style source. Tests pass the
// result straight to parseStateConfig, so the file is classified as
// FileConfig and ValidateFile runs the structural checks.
func parseConfig(t *testing.T, src string) *lang.File {
	t.Helper()
	f, err := lang.ParseSource("config.ub", []byte(src))
	require.NoError(t, err)
	f.Kind = lang.FileConfig
	errs := lang.ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "validate: %v", errs.Err())
	return f
}

func TestParseStateConfigNilFile(t *testing.T) {
	sc, err := parseStateConfig(nil, "")
	require.NoError(t, err)
	assert.Nil(t, sc.Backend)
	assert.Nil(t, sc.Encrypter)
}

func TestParseStateConfigAbsentBlock(t *testing.T) {
	f := parseConfig(t, "inputs: { x: 1 }\n")
	sc, err := parseStateConfig(f, "config.ub")
	require.NoError(t, err)
	assert.Nil(t, sc.Backend)
	assert.Nil(t, sc.Encrypter)
}

func TestValidateRejectsDottedBackend(t *testing.T) {
	f, err := lang.ParseSource(
		"config.ub", []byte("state: { @backend: core.local, path: '.unobin/state' }\n"))
	require.NoError(t, err)
	f.Kind = lang.FileConfig
	errs := lang.ValidateFile(f)
	require.NotZero(t, errs.Len())
	require.Contains(t, errs.Err().Error(), "bare name")
}

func TestParseStateConfigBackendAndEncryption(t *testing.T) {
	src := `
state: {
  @backend: local
  path:     '/tmp/state'
}

encryption: { @key-source: noop }
`
	f := parseConfig(t, src)
	sc, err := parseStateConfig(f, "config.ub")
	require.NoError(t, err)
	require.NotNil(t, sc.Backend)
	assert.Equal(t, "local", sc.Backend.Name)
	assert.Equal(t, "/tmp/state", sc.Backend.Body["path"])
	require.NotNil(t, sc.Encrypter)
	assert.Equal(t, "noop", sc.Encrypter.Name)
}

func TestParseStateConfigEncryptionOnly(t *testing.T) {
	f := parseConfig(t, "encryption: { @key-source: noop }\n")
	sc, err := parseStateConfig(f, "config.ub")
	require.NoError(t, err)
	assert.Nil(t, sc.Backend)
	require.NotNil(t, sc.Encrypter)
	assert.Equal(t, "noop", sc.Encrypter.Name)
}

func TestValidateRejectsEncryptionInsideState(t *testing.T) {
	f, err := lang.ParseSource("config.ub", []byte(
		"state: { @backend: local, encryption: { @key-source: noop } }\n"))
	require.NoError(t, err)
	f.Kind = lang.FileConfig
	errs := lang.ValidateFile(f)
	require.NotZero(t, errs.Len())
	require.Contains(t, errs.Err().Error(), "its own top-level block")
}

func TestResolveBackendNilRefIsError(t *testing.T) {
	_, err := resolveBackend(nil, "test-stack", "default", envencrypt.Noop{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be configured")
}

func TestResolveBackendLocal(t *testing.T) {
	dir := t.TempDir()
	ref := &resolverRef{Name: "local", Body: map[string]any{"path": dir}}
	b, err := resolveBackend(ref, "test-stack", "default", envencrypt.Noop{})
	require.NoError(t, err)
	require.NotNil(t, b)
}

func TestResolveBackendRejectsUnknownName(t *testing.T) {
	ref := &resolverRef{Name: "ghost"}
	_, err := resolveBackend(ref, "test-stack", "default", envencrypt.Noop{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no backend named "ghost"`)
	assert.Contains(t, err.Error(), "available: local")
}

func TestResolveBackendRejectsBadConfig(t *testing.T) {
	ref := &resolverRef{Name: "local", Body: map[string]any{"unknown": 1}}
	_, err := resolveBackend(ref, "test-stack", "default", envencrypt.Noop{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state:")
}

func TestResolveEncrypterNilNoEnvKey(t *testing.T) {
	t.Setenv("UB_STATE_KEY", "")
	enc, err := resolveEncrypter(nil)
	require.NoError(t, err)
	_, ok := enc.(envencrypt.Noop)
	assert.True(t, ok, "expected Noop, got %T", enc)
}

func TestResolveEncrypterNilUsesEnvKey(t *testing.T) {
	t.Setenv("UB_STATE_KEY", "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=")
	enc, err := resolveEncrypter(nil)
	require.NoError(t, err)
	_, isNoop := enc.(envencrypt.Noop)
	assert.False(t, isNoop, "expected an env-key encrypter, got Noop")

	probe := []byte("hello")
	sealed, err := enc.Encrypt(probe)
	require.NoError(t, err)
	opened, err := enc.Decrypt(sealed)
	require.NoError(t, err)
	assert.Equal(t, probe, opened)
}

func TestResolveEncrypterNamed(t *testing.T) {
	ref := &resolverRef{Name: "noop"}
	enc, err := resolveEncrypter(ref)
	require.NoError(t, err)
	_, ok := enc.(envencrypt.Noop)
	assert.True(t, ok, "expected Noop, got %T", enc)
}

func TestResolveEncrypterRejectsUnknownName(t *testing.T) {
	ref := &resolverRef{Name: "ghost"}
	_, err := resolveEncrypter(ref)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no key-source named "ghost"`)
	assert.Contains(t, err.Error(), "available: env-key, noop")
}
