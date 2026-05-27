package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.ub")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func parseTestConfig(t *testing.T, path string) *lang.File {
	t.Helper()
	f, err := parseConfigFile(path)
	require.NoError(t, err)
	return f
}

func TestLoadStackEnvelopeNilFile(t *testing.T) {
	env, err := loadStackEnvelope(nil, "")
	require.NoError(t, err)
	assert.False(t, env.Present)
	assert.Empty(t, env.ModulePath)
	assert.Empty(t, env.SupportedVersions)
}

func TestLoadStackEnvelopeNoStackBlock(t *testing.T) {
	path := writeConfig(t, `inputs: { region: 'us-east-1' }`)
	env, err := loadStackEnvelope(parseTestConfig(t, path), path)
	require.NoError(t, err)
	assert.False(t, env.Present)
}

func TestLoadStackEnvelopeWithStackBlock(t *testing.T) {
	path := writeConfig(t, `
stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', content-revision: 'abcdef' },
    { version: 'v0.2.0', content-revision: '123456' },
  ]
}`)
	env, err := loadStackEnvelope(parseTestConfig(t, path), path)
	require.NoError(t, err)
	assert.True(t, env.Present)
	assert.Equal(t, "github.com/cloudboss/cluster-deploy", env.ModulePath)
	assert.Equal(t, []supportedVersion{
		{Version: "v0.1.0", ContentRevision: "abcdef"},
		{Version: "v0.2.0", ContentRevision: "123456"},
	}, env.SupportedVersions)
}

func TestLoadStackEnvelopeStackBlockWithoutSupportedVersions(t *testing.T) {
	path := writeConfig(t, `
stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
}`)
	env, err := loadStackEnvelope(parseTestConfig(t, path), path)
	require.NoError(t, err)
	assert.True(t, env.Present)
	assert.Equal(t, "github.com/cloudboss/cluster-deploy", env.ModulePath)
	assert.Empty(t, env.SupportedVersions)
}

func TestVerifyStackEnvelopeNoConfigSoftFails(t *testing.T) {
	info := Info{
		StackName:       "test-stack",
		StackVersion:    "v0.1.0",
		ContentRevision: "abcdef",
		ModulePath:      "github.com/cloudboss/test-stack",
	}
	err := verifyStackEnvelope(info, nil, "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
}

func TestVerifyStackEnvelopeNoConfigOverrideAllows(t *testing.T) {
	info := Info{StackVersion: "v0.1.0", ContentRevision: "abcdef"}
	require.NoError(t, verifyStackEnvelope(info, nil, "", true))
}

func TestVerifyStackEnvelopeMissingStackBlockSoftFails(t *testing.T) {
	info := Info{StackVersion: "v0.1.0", ContentRevision: "abcdef"}
	path := writeConfig(t, `inputs: { region: 'us-east-1' }`)
	err := verifyStackEnvelope(info, parseTestConfig(t, path), path, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
}

func TestVerifyStackEnvelopeEmptySupportedVersionsSoftFails(t *testing.T) {
	info := Info{StackVersion: "v0.1.0", ContentRevision: "abcdef"}
	path := writeConfig(t, `
stack: {
  module-path: 'github.com/cloudboss/test'
  supported-versions: []
}`)
	info.ModulePath = "github.com/cloudboss/test"
	err := verifyStackEnvelope(info, parseTestConfig(t, path), path, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
}

func TestVerifyStackEnvelopeVersionNotInListSoftFails(t *testing.T) {
	info := Info{
		StackName:       "test",
		StackVersion:    "v0.9.0",
		ContentRevision: "ffffff",
		ModulePath:      "github.com/cloudboss/test",
	}
	path := writeConfig(t, `
stack: {
  module-path: 'github.com/cloudboss/test'
  supported-versions: [
    { version: 'v0.1.0'  content-revision: 'abcdef' }
  ]
}`)
	err := verifyStackEnvelope(info, parseTestConfig(t, path), path, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
	assert.Contains(t, err.Error(), "v0.9.0")
}

func TestVerifyStackEnvelopeVersionMismatchOverrideAllows(t *testing.T) {
	info := Info{
		StackVersion:    "v0.9.0",
		ContentRevision: "ffffff",
		ModulePath:      "github.com/cloudboss/test",
	}
	path := writeConfig(t, `
stack: {
  module-path: 'github.com/cloudboss/test'
  supported-versions: [
    { version: 'v0.1.0'  content-revision: 'abcdef' }
  ]
}`)
	require.NoError(t, verifyStackEnvelope(info, parseTestConfig(t, path), path, true))
}

func TestVerifyStackEnvelopeModulePathMismatchHardFails(t *testing.T) {
	info := Info{
		StackVersion:    "v0.1.0",
		ContentRevision: "abcdef",
		ModulePath:      "github.com/cloudboss/binary-source",
	}
	path := writeConfig(t, `
stack: {
  module-path: 'github.com/cloudboss/different-source'
  supported-versions: [
    { version: 'v0.1.0'  content-revision: 'abcdef' }
  ]
}`)
	err := verifyStackEnvelope(info, parseTestConfig(t, path), path, false)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "--allow-version-mismatch")
	assert.Contains(t, err.Error(), "different-source")
}

func TestVerifyStackEnvelopeModulePathMismatchNotOverridable(t *testing.T) {
	info := Info{
		StackVersion:    "v0.1.0",
		ContentRevision: "abcdef",
		ModulePath:      "github.com/cloudboss/binary-source",
	}
	path := writeConfig(t, `
stack: {
  module-path: 'github.com/cloudboss/different-source'
  supported-versions: [
    { version: 'v0.1.0'  content-revision: 'abcdef' }
  ]
}`)
	err := verifyStackEnvelope(info, parseTestConfig(t, path), path, true)
	require.Error(t, err)
}

func TestVerifyStackEnvelopeMatchingPinPasses(t *testing.T) {
	info := Info{
		StackVersion:    "v0.1.0",
		ContentRevision: "abcdef",
		ModulePath:      "github.com/cloudboss/test",
	}
	path := writeConfig(t, `
stack: {
  module-path: 'github.com/cloudboss/test'
  supported-versions: [
    { version: 'v0.1.0'  content-revision: 'abcdef' }
  ]
}`)
	require.NoError(t, verifyStackEnvelope(info, parseTestConfig(t, path), path, false))
}

func TestVerifyStackEnvelopeNoModulePathFieldChecksOnlyPin(t *testing.T) {
	info := Info{
		StackVersion:    "v0.1.0",
		ContentRevision: "abcdef",
		ModulePath:      "github.com/cloudboss/test",
	}
	path := writeConfig(t, `
stack: {
  supported-versions: [
    { version: 'v0.1.0'  content-revision: 'abcdef' }
  ]
}`)
	require.NoError(t, verifyStackEnvelope(info, parseTestConfig(t, path), path, false))
}

// TestVerifyStackEnvelopeComparesAgainstModulePathNotBody guards against
// the regression where the identity check compared the config's
// module-path against the embedded source bytes (which are a multi-line
// .ub file, never a URL). It's the realistic shape: StackBody holds
// multi-line source, ModulePath holds a clean URL, the config's
// module-path matches the URL.
func TestVerifyStackEnvelopeComparesAgainstModulePathNotBody(t *testing.T) {
	info := Info{
		StackVersion:    "v0.1.0",
		ContentRevision: "abcdef",
		ModulePath:      "github.com/cloudboss/cluster-deploy",
		StackBody: `
inputs: { region: { type: string } }
resources: {
  local: { file: { x: { path: '/tmp/x', content: 'hi' } } }
}
`,
	}
	path := writeConfig(t, `
stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', content-revision: 'abcdef' }
  ]
}`)
	require.NoError(t, verifyStackEnvelope(info, parseTestConfig(t, path), path, false))
}
