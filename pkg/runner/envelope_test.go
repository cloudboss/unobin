package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.ub")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoadStackEnvelopeEmptyPath(t *testing.T) {
	env, err := loadStackEnvelope("")
	require.NoError(t, err)
	assert.False(t, env.Present)
	assert.Empty(t, env.Source)
	assert.Empty(t, env.SupportedVersions)
}

func TestLoadStackEnvelopeNoStackBlock(t *testing.T) {
	path := writeConfig(t, `inputs: { region: 'us-east-1' }`)
	env, err := loadStackEnvelope(path)
	require.NoError(t, err)
	assert.False(t, env.Present)
}

func TestLoadStackEnvelopeWithStackBlock(t *testing.T) {
	path := writeConfig(t, `
stack: {
  source: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', commit: 'abcdef' },
    { version: 'v0.2.0', commit: '123456' },
  ]
}`)
	env, err := loadStackEnvelope(path)
	require.NoError(t, err)
	assert.True(t, env.Present)
	assert.Equal(t, "github.com/cloudboss/cluster-deploy", env.Source)
	assert.Equal(t, []supportedVersion{
		{Version: "v0.1.0", Commit: "abcdef"},
		{Version: "v0.2.0", Commit: "123456"},
	}, env.SupportedVersions)
}

func TestLoadStackEnvelopeStackBlockWithoutSupportedVersions(t *testing.T) {
	path := writeConfig(t, `
stack: {
  source: 'github.com/cloudboss/cluster-deploy'
}`)
	env, err := loadStackEnvelope(path)
	require.NoError(t, err)
	assert.True(t, env.Present)
	assert.Equal(t, "github.com/cloudboss/cluster-deploy", env.Source)
	assert.Empty(t, env.SupportedVersions)
}

func TestVerifyStackEnvelopeNoConfigSoftFails(t *testing.T) {
	info := Info{
		StackName:    "test-stack",
		StackVersion: "v0.1.0",
		StackCommit:  "abcdef",
		StackBody:  "github.com/cloudboss/test-stack",
	}
	err := verifyStackEnvelope(info, "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
}

func TestVerifyStackEnvelopeNoConfigOverrideAllows(t *testing.T) {
	info := Info{StackVersion: "v0.1.0", StackCommit: "abcdef"}
	require.NoError(t, verifyStackEnvelope(info, "", true))
}

func TestVerifyStackEnvelopeMissingStackBlockSoftFails(t *testing.T) {
	info := Info{StackVersion: "v0.1.0", StackCommit: "abcdef"}
	path := writeConfig(t, `inputs: { region: 'us-east-1' }`)
	err := verifyStackEnvelope(info, path, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
}

func TestVerifyStackEnvelopeEmptySupportedVersionsSoftFails(t *testing.T) {
	info := Info{StackVersion: "v0.1.0", StackCommit: "abcdef"}
	path := writeConfig(t, `
stack: {
  source: 'github.com/cloudboss/test'
  supported-versions: []
}`)
	info.StackBody = "github.com/cloudboss/test"
	err := verifyStackEnvelope(info, path, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
}

func TestVerifyStackEnvelopeVersionNotInListSoftFails(t *testing.T) {
	info := Info{
		StackName:    "test",
		StackVersion: "v0.9.0",
		StackCommit:  "ffffff",
		StackBody:  "github.com/cloudboss/test",
	}
	path := writeConfig(t, `
stack: {
  source: 'github.com/cloudboss/test'
  supported-versions: [
    { version: 'v0.1.0'  commit: 'abcdef' }
  ]
}`)
	err := verifyStackEnvelope(info, path, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
	assert.Contains(t, err.Error(), "v0.9.0")
}

func TestVerifyStackEnvelopeVersionMismatchOverrideAllows(t *testing.T) {
	info := Info{
		StackVersion: "v0.9.0",
		StackCommit:  "ffffff",
		StackBody:  "github.com/cloudboss/test",
	}
	path := writeConfig(t, `
stack: {
  source: 'github.com/cloudboss/test'
  supported-versions: [
    { version: 'v0.1.0'  commit: 'abcdef' }
  ]
}`)
	require.NoError(t, verifyStackEnvelope(info, path, true))
}

func TestVerifyStackEnvelopeSourceMismatchHardFails(t *testing.T) {
	info := Info{
		StackVersion: "v0.1.0",
		StackCommit:  "abcdef",
		StackBody:  "github.com/cloudboss/binary-source",
	}
	path := writeConfig(t, `
stack: {
  source: 'github.com/cloudboss/different-source'
  supported-versions: [
    { version: 'v0.1.0'  commit: 'abcdef' }
  ]
}`)
	err := verifyStackEnvelope(info, path, false)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "--allow-version-mismatch")
	assert.Contains(t, err.Error(), "different-source")
}

func TestVerifyStackEnvelopeSourceMismatchNotOverridable(t *testing.T) {
	info := Info{
		StackVersion: "v0.1.0",
		StackCommit:  "abcdef",
		StackBody:  "github.com/cloudboss/binary-source",
	}
	path := writeConfig(t, `
stack: {
  source: 'github.com/cloudboss/different-source'
  supported-versions: [
    { version: 'v0.1.0'  commit: 'abcdef' }
  ]
}`)
	err := verifyStackEnvelope(info, path, true)
	require.Error(t, err)
}

func TestVerifyStackEnvelopeMatchingPinPasses(t *testing.T) {
	info := Info{
		StackVersion: "v0.1.0",
		StackCommit:  "abcdef",
		StackBody:  "github.com/cloudboss/test",
	}
	path := writeConfig(t, `
stack: {
  source: 'github.com/cloudboss/test'
  supported-versions: [
    { version: 'v0.1.0'  commit: 'abcdef' }
  ]
}`)
	require.NoError(t, verifyStackEnvelope(info, path, false))
}

func TestVerifyStackEnvelopeNoSourceFieldChecksOnlyPin(t *testing.T) {
	info := Info{
		StackVersion: "v0.1.0",
		StackCommit:  "abcdef",
		StackBody:  "github.com/cloudboss/test",
	}
	path := writeConfig(t, `
stack: {
  supported-versions: [
    { version: 'v0.1.0'  commit: 'abcdef' }
  ]
}`)
	require.NoError(t, verifyStackEnvelope(info, path, false))
}
