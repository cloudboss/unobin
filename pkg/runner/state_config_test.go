package runner

import (
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	s3store "github.com/cloudboss/unobin/pkg/state/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseStackFixture(t *testing.T, src string) *parsedStack {
	t.Helper()
	config, err := parseStackSource("stack.ub", []byte(sourceStackWithNoop(src)))
	require.NoError(t, err)
	return config
}

func TestParseStateConfigNilFile(t *testing.T) {
	sc, err := parseStateConfig(nil, "")
	require.NoError(t, err)
	assert.Nil(t, sc.Backend)
	assert.Nil(t, sc.Encrypter)
}

func TestParseStateConfigAbsentBlock(t *testing.T) {
	config := &parsedStack{stack: &syntax.StackFile{}}
	sc, err := parseStateConfig(config, "stack.ub")
	require.NoError(t, err)
	assert.Nil(t, sc.Backend)
	assert.Nil(t, sc.Encrypter)
}

func TestValidateRejectsDottedBackend(t *testing.T) {
	_, err := parseStackSource("stack.ub", []byte(`
stack: {
  state: core.local { path: '.unobin/state' }
  encryption: noop {}
}
`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "state selector must have one segment")
}

func TestParseStateConfigBackendAndEncryption(t *testing.T) {
	src := `
state: local {
  path: '/tmp/state'
}

encryption: noop {}
`
	f := parseStackFixture(t, src)
	sc, err := parseStateConfig(f, "stack.ub")
	require.NoError(t, err)
	require.NotNil(t, sc.Backend)
	assert.Equal(t, "local", sc.Backend.Name)
	assert.Equal(t, "/tmp/state", sc.Backend.Body["path"])
	require.NotNil(t, sc.Encrypter)
	assert.Equal(t, "noop", sc.Encrypter.Name)
}

func TestParseStackFileAcceptsSourceStack(t *testing.T) {
	path := writeConfig(t, `
stack: {
  locals: { state-path: '/tmp/state' }

  factory: {
    inputs: { region: 'us-east-1' }
  }

  state: local {
    path: local.state-path
  }

  encryption: noop {}

  parallelism: 3
}
`)

	config, err := parseStackFile(path)
	require.NoError(t, err)
	require.NotNil(t, stackFile(config))

	sc, err := parseStateConfig(config, path)
	require.NoError(t, err)
	require.NotNil(t, sc.Backend)
	assert.Equal(t, "local", sc.Backend.Name)
	assert.Equal(t, "/tmp/state", sc.Backend.Body["path"])
	require.NotNil(t, sc.Encrypter)
	assert.Equal(t, "noop", sc.Encrypter.Name)

	parallelism, err := loadParallelism(config, path)
	require.NoError(t, err)
	assert.Equal(t, 3, parallelism)
}

func TestParseStateConfigEncryptionOnly(t *testing.T) {
	config := parseStackFixture(t, "encryption: noop {}\n")
	sc, err := parseStateConfig(config, "stack.ub")
	require.NoError(t, err)
	assert.Nil(t, sc.Backend)
	require.NotNil(t, sc.Encrypter)
	assert.Equal(t, "noop", sc.Encrypter.Name)
}

func TestValidateRejectsEncryptionInsideState(t *testing.T) {
	_, err := parseStackSource("stack.ub", []byte(`
stack: {
  state: local {
    encryption: noop {}
  }
  encryption: noop {}
}
`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "its own top-level block")
}

func TestResolveBackendNilRefIsError(t *testing.T) {
	_, err := resolveBackend(nil, "test-stack", "default", encrypters.Noop{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be configured")
}

func TestResolveBackendLocal(t *testing.T) {
	dir := t.TempDir()
	ref := &resolverRef{Name: "local", Body: map[string]any{"path": dir}}
	b, err := resolveBackend(ref, "test-stack", "default", encrypters.Noop{})
	require.NoError(t, err)
	require.NotNil(t, b)
}

func TestResolveBackendRejectsUnknownName(t *testing.T) {
	ref := &resolverRef{Name: "ghost"}
	_, err := resolveBackend(ref, "test-stack", "default", encrypters.Noop{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no backend named "ghost"`)
	assert.Contains(t, err.Error(), "available: local, s3")
}

func TestResolveBackendS3(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	ref := &resolverRef{Name: "s3", Body: map[string]any{
		"bucket": "acme-state",
		"prefix": "unobin",
		"aws":    map[string]any{"region": "us-east-1"},
	}}
	b, err := resolveBackend(ref, "test-factory", "default", encrypters.Noop{})
	require.NoError(t, err)
	require.IsType(t, &s3store.Store{}, b)
}

func TestResolveBackendS3RejectsUnknownKey(t *testing.T) {
	ref := &resolverRef{Name: "s3", Body: map[string]any{
		"bucket": "acme-state",
		"region": "us-east-1",
	}}
	_, err := resolveBackend(ref, "test-factory", "default", encrypters.Noop{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown key region")
}

func TestResolveBackendRejectsBadConfig(t *testing.T) {
	ref := &resolverRef{Name: "local", Body: map[string]any{"unknown": 1}}
	_, err := resolveBackend(ref, "test-stack", "default", encrypters.Noop{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state:")
}

func TestResolveEncrypterNilNoEnvKey(t *testing.T) {
	t.Setenv("UB_STATE_KEY", "")
	enc, err := resolveEncrypter(nil)
	require.NoError(t, err)
	_, ok := enc.(encrypters.Noop)
	assert.True(t, ok, "expected Noop, got %T", enc)
}

func TestResolveEncrypterNilUsesEnvKey(t *testing.T) {
	t.Setenv("UB_STATE_KEY", "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=")
	enc, err := resolveEncrypter(nil)
	require.NoError(t, err)
	_, isNoop := enc.(encrypters.Noop)
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
	_, ok := enc.(encrypters.Noop)
	assert.True(t, ok, "expected Noop, got %T", enc)
}

func TestResolveEncrypterRejectsUnknownName(t *testing.T) {
	ref := &resolverRef{Name: "ghost"}
	_, err := resolveEncrypter(ref)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no key-source named "ghost"`)
	assert.Contains(t, err.Error(), "available: env-key, kms, noop")
}

func TestResolveEncrypterKMS(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	ref := &resolverRef{Name: "kms", Body: map[string]any{
		"key-id": "alias/unobin-state",
		"aws":    map[string]any{"region": "us-east-1"},
	}}
	enc, err := resolveEncrypter(ref)
	require.NoError(t, err)
	require.IsType(t, &encrypters.KMS{}, enc)
}

func TestParseStateConfigResolvesLocals(t *testing.T) {
	src := `
locals: {
  aws-config: {
    assume-role: {
      role-arn:    'arn:aws:iam::123456789012:role/unobin-state'
      external-id: 'unobin-test'
    }
  }
}

state: s3 {
  bucket: 'cloudboss'
  aws:    local.aws-config
}

encryption: kms {
  key-id: 'alias/cloudboss'
  aws:    local.aws-config
}
`
	f := parseStackFixture(t, src)
	sc, err := parseStateConfig(f, "stack.ub")
	require.NoError(t, err)
	want := map[string]any{
		"assume-role": map[string]any{
			"role-arn":    "arn:aws:iam::123456789012:role/unobin-state",
			"external-id": "unobin-test",
		},
	}
	require.NotNil(t, sc.Backend)
	assert.Equal(t, "s3", sc.Backend.Name)
	assert.Equal(t, "cloudboss", sc.Backend.Body["bucket"])
	assert.Equal(t, want, sc.Backend.Body["aws"])
	require.NotNil(t, sc.Encrypter)
	assert.Equal(t, "kms", sc.Encrypter.Name)
	assert.Equal(t, "alias/cloudboss", sc.Encrypter.Body["key-id"])
	assert.Equal(t, want, sc.Encrypter.Body["aws"])
}
