package runner

import (
	"os"
	"testing"

	"github.com/cloudboss/unobin/pkg/envencrypt"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/libraries/core"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseConfig parses an in-memory config.ub-style source. Tests pass
// the result straight to parseStateConfig, so the file is classified
// as FileConfig and ValidateFile runs the structural checks.
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

func TestParseStateConfigBareBackend(t *testing.T) {
	f := parseConfig(t, "state: { @backend: local, path: '.unobin/state' }\n")
	sc, err := parseStateConfig(f, "config.ub")
	require.NoError(t, err)
	require.NotNil(t, sc.Backend)
	assert.Equal(t, "", sc.Backend.Alias)
	assert.Equal(t, "local", sc.Backend.Name)
	assert.Equal(t, map[string]any{"path": ".unobin/state"}, sc.Backend.Body)
	assert.Nil(t, sc.Encrypter)
}

func TestParseStateConfigAliasedBackendAndEncryption(t *testing.T) {
	src := `
state: {
  @backend: aws.s3
  bucket:   'tf-state'
  region:   'us-east-1'
  encryption: { @key-source: aws.kms, key-id: 'alias/state' }
}
`
	f := parseConfig(t, src)
	sc, err := parseStateConfig(f, "config.ub")
	require.NoError(t, err)
	require.NotNil(t, sc.Backend)
	assert.Equal(t, "aws", sc.Backend.Alias)
	assert.Equal(t, "s3", sc.Backend.Name)
	assert.Equal(t, "tf-state", sc.Backend.Body["bucket"])
	assert.Equal(t, "us-east-1", sc.Backend.Body["region"])
	require.NotNil(t, sc.Encrypter)
	assert.Equal(t, "aws", sc.Encrypter.Alias)
	assert.Equal(t, "kms", sc.Encrypter.Name)
	assert.Equal(t, "alias/state", sc.Encrypter.Body["key-id"])
}

// fakeBackendConfig matches the body the fakeBackend test fixture
// accepts, so cfg.Decode rejects unknown fields.
type fakeBackendConfig struct {
	Path cfg.String
}

type fakeBackend struct{ sdkstate.Backend }

func newFakeBackend(_ any, _, _ string, _ sdkencrypt.Encrypter) (sdkstate.Backend, error) {
	return &fakeBackend{}, nil
}

type fakeEncrypterConfig struct {
	EnvVar cfg.String
}

type fakeEncrypter struct{}

func (fakeEncrypter) Encrypt(b []byte) ([]byte, error) { return b, nil }
func (fakeEncrypter) Decrypt(b []byte) ([]byte, error) { return b, nil }

func newFakeEncrypter(_ any) (sdkencrypt.Encrypter, error) { return fakeEncrypter{}, nil }

func fakeProviderLibrary() *runtime.Library {
	return &runtime.Library{
		Name: "fake",
		StateBackends: map[string]sdkstate.BackendType{
			"store": {
				Name: "store",
				Configuration: &cfg.ConfigurationType{
					New: func() any { return &fakeBackendConfig{} },
				},
				New: newFakeBackend,
			},
		},
		Encrypters: map[string]sdkencrypt.EncrypterType{
			"keysrc": {
				Name: "keysrc",
				Configuration: &cfg.ConfigurationType{
					New: func() any { return &fakeEncrypterConfig{} },
				},
				New: newFakeEncrypter,
			},
		},
	}
}

func resolverInfo() Info {
	return Info{
		StackName: "test-stack",
		Libraries: map[string]*runtime.Library{
			"core": core.Library(),
			"fake": fakeProviderLibrary(),
		},
	}
}

func TestResolveBackendNilRefFallsBackToCoreLocal(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(wd) })
	require.NoError(t, os.Chdir(dir))

	b, err := resolveBackend(resolverInfo(), nil, "test-stack", "default", envencrypt.Noop{})
	require.NoError(t, err)
	require.NotNil(t, b)
}

func TestResolveBackendCoreLocal(t *testing.T) {
	dir := t.TempDir()
	ref := &resolverRef{Name: "local", Body: map[string]any{"path": dir}}
	b, err := resolveBackend(resolverInfo(), ref, "test-stack", "default", envencrypt.Noop{})
	require.NoError(t, err)
	require.NotNil(t, b)
}

func TestResolveBackendAliasedProvider(t *testing.T) {
	ref := &resolverRef{Alias: "fake", Name: "store", Body: map[string]any{"path": "/tmp"}}
	b, err := resolveBackend(resolverInfo(), ref, "test-stack", "default", envencrypt.Noop{})
	require.NoError(t, err)
	require.NotNil(t, b)
}

func TestResolveBackendRejectsMissingAlias(t *testing.T) {
	ref := &resolverRef{Alias: "missing", Name: "store"}
	_, err := resolveBackend(resolverInfo(), ref, "test-stack", "default", envencrypt.Noop{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "import \"missing\" not found")
}

func TestResolveBackendRejectsUnknownName(t *testing.T) {
	ref := &resolverRef{Alias: "fake", Name: "ghost"}
	_, err := resolveBackend(resolverInfo(), ref, "test-stack", "default", envencrypt.Noop{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `registers no backend named "ghost"`)
}

func TestResolveBackendRejectsBadConfig(t *testing.T) {
	ref := &resolverRef{Alias: "fake", Name: "store", Body: map[string]any{"unknown": 1}}
	_, err := resolveBackend(resolverInfo(), ref, "test-stack", "default", envencrypt.Noop{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state:")
}

func TestResolveEncrypterNilNoEnvKey(t *testing.T) {
	t.Setenv("UB_STATE_KEY", "")
	enc, err := resolveEncrypter(resolverInfo(), nil)
	require.NoError(t, err)
	_, ok := enc.(envencrypt.Noop)
	assert.True(t, ok, "expected Noop, got %T", enc)
}

func TestResolveEncrypterNilUsesEnvKey(t *testing.T) {
	t.Setenv("UB_STATE_KEY", "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=")
	enc, err := resolveEncrypter(resolverInfo(), nil)
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

func TestResolveEncrypterAliasedProvider(t *testing.T) {
	ref := &resolverRef{Alias: "fake", Name: "keysrc", Body: map[string]any{"env-var": "X"}}
	enc, err := resolveEncrypter(resolverInfo(), ref)
	require.NoError(t, err)
	_, ok := enc.(fakeEncrypter)
	assert.True(t, ok, "expected fakeEncrypter, got %T", enc)
}

func TestResolveEncrypterRejectsMissingAlias(t *testing.T) {
	ref := &resolverRef{Alias: "missing", Name: "keysrc"}
	_, err := resolveEncrypter(resolverInfo(), ref)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "import \"missing\" not found")
}

func TestResolveEncrypterRejectsUnknownName(t *testing.T) {
	ref := &resolverRef{Alias: "fake", Name: "ghost"}
	_, err := resolveEncrypter(resolverInfo(), ref)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `registers no encrypter named "ghost"`)
}

var _ sdkstate.Backend = (*fakeBackend)(nil)
