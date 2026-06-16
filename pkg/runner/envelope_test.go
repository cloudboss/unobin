package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dev.ub")
	require.NoError(t, os.WriteFile(path, []byte(sourceStackWithNoop(body)), 0o600))
	return path
}

func sourceStack(body string) string {
	if strings.HasPrefix(strings.TrimSpace(body), "stack:") {
		return body
	}
	return "stack: {\n" + body + "\n}\n"
}

func sourceStackWithNoop(body string) string {
	if strings.HasPrefix(strings.TrimSpace(body), "stack:") {
		return body
	}
	if !strings.Contains(body, "encryption:") {
		body += "\n\nencryption: noop {}\n"
	}
	return sourceStack(body)
}

func parseTestConfig(t *testing.T, path string) *parsedConfig {
	t.Helper()
	config, err := parseConfigFile(path)
	require.NoError(t, err)
	return config
}

func TestLoadFactoryEnvelopeNilFile(t *testing.T) {
	env, err := loadFactoryEnvelope(nil, "")
	require.NoError(t, err)
	assert.False(t, env.Present)
	assert.Empty(t, env.LibraryPath)
	assert.Empty(t, env.SupportedVersions)
}

func TestLoadFactoryEnvelopeNoFactoryBlock(t *testing.T) {
	path := writeConfig(t, `locals: { region: 'us-east-1' }`)
	env, err := loadFactoryEnvelope(parseTestConfig(t, path), path)
	require.NoError(t, err)
	assert.False(t, env.Present)
}

func TestLoadFactoryEnvelopeWithPin(t *testing.T) {
	path := writeConfig(t, `
factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.1.0', content-revision: 'abcdef' },
      { version: 'v0.2.0', content-revision: '123456' },
    ]
  }
}`)
	env, err := loadFactoryEnvelope(parseTestConfig(t, path), path)
	require.NoError(t, err)
	assert.True(t, env.Present)
	assert.Equal(t, "github.com/cloudboss/cluster-deploy", env.LibraryPath)
	assert.Equal(t, []supportedVersion{
		{Version: "v0.1.0", ContentRevision: "abcdef"},
		{Version: "v0.2.0", ContentRevision: "123456"},
	}, env.SupportedVersions)
}

func TestLoadFactoryEnvelopeFactoryWithoutPin(t *testing.T) {
	path := writeConfig(t, `
factory: {
  inputs: { region: 'us-east-1' }
}`)
	env, err := loadFactoryEnvelope(parseTestConfig(t, path), path)
	require.NoError(t, err)
	assert.True(t, env.Present)
	assert.Empty(t, env.LibraryPath)
	assert.Empty(t, env.SupportedVersions)
}

func TestLoadFactoryEnvelopePinWithoutSupportedVersions(t *testing.T) {
	path := writeConfig(t, `
factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
  }
}`)
	env, err := loadFactoryEnvelope(parseTestConfig(t, path), path)
	require.NoError(t, err)
	assert.True(t, env.Present)
	assert.Equal(t, "github.com/cloudboss/cluster-deploy", env.LibraryPath)
	assert.Empty(t, env.SupportedVersions)
}

func TestVerifyFactoryEnvelopeNoConfigSoftFails(t *testing.T) {
	info := Info{
		FactoryName:     "test-stack",
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		LibraryPath:     "github.com/cloudboss/test-stack",
	}
	err := verifyFactoryEnvelope(info, nil, "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
}

func TestVerifyFactoryEnvelopeNoConfigOverrideAllows(t *testing.T) {
	info := Info{FactoryVersion: "v0.1.0", ContentRevision: "abcdef"}
	require.NoError(t, verifyFactoryEnvelope(info, nil, "", true))
}

func TestVerifyFactoryEnvelopeMissingFactoryBlockSoftFails(t *testing.T) {
	info := Info{FactoryVersion: "v0.1.0", ContentRevision: "abcdef"}
	path := writeConfig(t, `locals: { region: 'us-east-1' }`)
	err := verifyFactoryEnvelope(info, parseTestConfig(t, path), path, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
}

func TestVerifyFactoryEnvelopeEmptySupportedVersionsSoftFails(t *testing.T) {
	info := Info{FactoryVersion: "v0.1.0", ContentRevision: "abcdef"}
	path := writeConfig(t, `
factory: {
  pin: {
    library-path: 'github.com/cloudboss/test'
    supported-versions: []
  }
}`)
	info.LibraryPath = "github.com/cloudboss/test"
	err := verifyFactoryEnvelope(info, parseTestConfig(t, path), path, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
}

func TestVerifyFactoryEnvelopeVersionNotInListSoftFails(t *testing.T) {
	info := Info{
		FactoryName:     "test",
		FactoryVersion:  "v0.9.0",
		ContentRevision: "ffffff",
		LibraryPath:     "github.com/cloudboss/test",
	}
	path := writeConfig(t, `
factory: {
  pin: {
    library-path: 'github.com/cloudboss/test'
    supported-versions: [
      { version: 'v0.1.0'  content-revision: 'abcdef' }
    ]
  }
}`)
	err := verifyFactoryEnvelope(info, parseTestConfig(t, path), path, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
	assert.Contains(t, err.Error(), "v0.9.0")
}

func TestVerifyFactoryEnvelopeVersionMismatchOverrideAllows(t *testing.T) {
	info := Info{
		FactoryVersion:  "v0.9.0",
		ContentRevision: "ffffff",
		LibraryPath:     "github.com/cloudboss/test",
	}
	path := writeConfig(t, `
factory: {
  pin: {
    library-path: 'github.com/cloudboss/test'
    supported-versions: [
      { version: 'v0.1.0'  content-revision: 'abcdef' }
    ]
  }
}`)
	require.NoError(t, verifyFactoryEnvelope(info, parseTestConfig(t, path), path, true))
}

func TestVerifyFactoryEnvelopeLibraryPathMismatchHardFails(t *testing.T) {
	info := Info{
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		LibraryPath:     "github.com/cloudboss/binary-source",
	}
	path := writeConfig(t, `
factory: {
  pin: {
    library-path: 'github.com/cloudboss/different-source'
    supported-versions: [
      { version: 'v0.1.0'  content-revision: 'abcdef' }
    ]
  }
}`)
	err := verifyFactoryEnvelope(info, parseTestConfig(t, path), path, false)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "--allow-version-mismatch")
	assert.Contains(t, err.Error(), "different-source")
}

func TestVerifyFactoryEnvelopeLibraryPathMismatchNotOverridable(t *testing.T) {
	info := Info{
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		LibraryPath:     "github.com/cloudboss/binary-source",
	}
	path := writeConfig(t, `
factory: {
  pin: {
    library-path: 'github.com/cloudboss/different-source'
    supported-versions: [
      { version: 'v0.1.0'  content-revision: 'abcdef' }
    ]
  }
}`)
	err := verifyFactoryEnvelope(info, parseTestConfig(t, path), path, true)
	require.Error(t, err)
}

func TestVerifyFactoryEnvelopeMatchingPinPasses(t *testing.T) {
	info := Info{
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		LibraryPath:     "github.com/cloudboss/test",
	}
	path := writeConfig(t, `
factory: {
  pin: {
    library-path: 'github.com/cloudboss/test'
    supported-versions: [
      { version: 'v0.1.0'  content-revision: 'abcdef' }
    ]
  }
}`)
	require.NoError(t, verifyFactoryEnvelope(info, parseTestConfig(t, path), path, false))
}

func TestVerifyFactoryEnvelopeNoLibraryPathFieldChecksOnlyVersions(t *testing.T) {
	info := Info{
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		LibraryPath:     "github.com/cloudboss/test",
	}
	path := writeConfig(t, `
factory: {
  pin: {
    supported-versions: [
      { version: 'v0.1.0'  content-revision: 'abcdef' }
    ]
  }
}`)
	require.NoError(t, verifyFactoryEnvelope(info, parseTestConfig(t, path), path, false))
}

// TestVerifyFactoryEnvelopeComparesAgainstLibraryPathNotBody guards against
// the regression where the identity check compared the config's
// library-path against the embedded source bytes (which are a multi-line
// .ub file, never a URL). It mirrors real use: FactoryBody holds
// multi-line source, LibraryPath holds a clean URL, the config's
// library-path matches the URL.
func TestVerifyFactoryEnvelopeComparesAgainstLibraryPathNotBody(t *testing.T) {
	info := Info{
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		LibraryPath:     "github.com/cloudboss/cluster-deploy",
		FactoryBody: sourceFactory(`
inputs:    { region: { type: string } }
resources: { x: local.file { path: '/tmp/x', content: 'hi' } }
`),
	}
	path := writeConfig(t, `
factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.1.0', content-revision: 'abcdef' }
    ]
  }
}`)
	require.NoError(t, verifyFactoryEnvelope(info, parseTestConfig(t, path), path, false))
}

func TestParseConfigRejectsLocalReferenceInPin(t *testing.T) {
	path := writeConfig(t, `
locals: { repo: 'github.com/cloudboss/cluster-deploy' }

factory: { pin: { library-path: local.repo } }
`)
	_, err := parseConfigFile(path)
	require.Error(t, err)
	require.Contains(t, err.Error(),
		"`factory.pin.library-path:` must be a string literal")
}
