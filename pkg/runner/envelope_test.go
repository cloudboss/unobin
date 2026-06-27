package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dev.ub")
	require.NoError(t, os.WriteFile(path, []byte(sourceStackWithNoop(body)), 0o600))
	return path
}

func stackEnvelopeFixture(t testing.TB, name string) string {
	t.Helper()
	path := filepath.Join("testdata/ub/stack-envelope", filepath.FromSlash(name)+".ub")
	require.FileExists(t, path)
	return path
}

func readStackEnvelopeFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadFixture(t,
		filepath.Join("testdata/ub/stack-envelope", filepath.FromSlash(name)+".ub"))
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

func parseTestStack(t *testing.T, path string) *parsedStack {
	t.Helper()
	config, err := parseStackFile(path)
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
	path := stackEnvelopeFixture(t, "valid/no-factory-block")
	env, err := loadFactoryEnvelope(parseTestStack(t, path), path)
	require.NoError(t, err)
	assert.False(t, env.Present)
}

func TestLoadFactoryEnvelopeWithPin(t *testing.T) {
	path := stackEnvelopeFixture(t, "valid/with-pin")
	env, err := loadFactoryEnvelope(parseTestStack(t, path), path)
	require.NoError(t, err)
	assert.True(t, env.Present)
	assert.Equal(t, "github.com/cloudboss/cluster-deploy", env.LibraryPath)
	assert.Equal(t, []supportedVersion{
		{Version: "v0.1.0", ContentRevision: "abcdef"},
		{Version: "v0.2.0", ContentRevision: "123456"},
	}, env.SupportedVersions)
}

func TestLoadFactoryEnvelopeFactoryWithoutPin(t *testing.T) {
	path := stackEnvelopeFixture(t, "valid/factory-without-pin")
	env, err := loadFactoryEnvelope(parseTestStack(t, path), path)
	require.NoError(t, err)
	assert.True(t, env.Present)
	assert.Empty(t, env.LibraryPath)
	assert.Empty(t, env.SupportedVersions)
}

func TestLoadFactoryEnvelopePinWithoutSupportedVersions(t *testing.T) {
	path := stackEnvelopeFixture(t, "valid/pin-without-supported-versions")
	env, err := loadFactoryEnvelope(parseTestStack(t, path), path)
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
	path := stackEnvelopeFixture(t, "valid/no-factory-block")
	err := verifyFactoryEnvelope(info, parseTestStack(t, path), path, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-version-mismatch")
}

func TestVerifyFactoryEnvelopeEmptySupportedVersionsSoftFails(t *testing.T) {
	info := Info{FactoryVersion: "v0.1.0", ContentRevision: "abcdef"}
	path := stackEnvelopeFixture(t, "valid/empty-supported-versions")
	info.LibraryPath = "github.com/cloudboss/test"
	err := verifyFactoryEnvelope(info, parseTestStack(t, path), path, false)
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
	path := stackEnvelopeFixture(t, "valid/version-not-in-list")
	err := verifyFactoryEnvelope(info, parseTestStack(t, path), path, false)
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
	path := stackEnvelopeFixture(t, "valid/version-not-in-list")
	require.NoError(t, verifyFactoryEnvelope(info, parseTestStack(t, path), path, true))
}

func TestVerifyFactoryEnvelopeLibraryPathMismatchHardFails(t *testing.T) {
	info := Info{
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		LibraryPath:     "github.com/cloudboss/binary-source",
	}
	path := stackEnvelopeFixture(t, "valid/library-path-mismatch")
	err := verifyFactoryEnvelope(info, parseTestStack(t, path), path, false)
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
	path := stackEnvelopeFixture(t, "valid/library-path-mismatch")
	err := verifyFactoryEnvelope(info, parseTestStack(t, path), path, true)
	require.Error(t, err)
}

func TestVerifyFactoryEnvelopeMatchingPinPasses(t *testing.T) {
	info := Info{
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		LibraryPath:     "github.com/cloudboss/test",
	}
	path := stackEnvelopeFixture(t, "valid/matching-pin")
	require.NoError(t, verifyFactoryEnvelope(info, parseTestStack(t, path), path, false))
}

func TestVerifyFactoryEnvelopeNoLibraryPathFieldChecksOnlyVersions(t *testing.T) {
	info := Info{
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		LibraryPath:     "github.com/cloudboss/test",
	}
	path := stackEnvelopeFixture(t, "valid/no-library-path")
	require.NoError(t, verifyFactoryEnvelope(info, parseTestStack(t, path), path, false))
}

func TestVerifyFactoryEnvelopeComparesAgainstLibraryPath(t *testing.T) {
	info := Info{
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		LibraryPath:     "github.com/cloudboss/cluster-deploy",
	}
	path := stackEnvelopeFixture(t, "valid/with-pin")
	require.NoError(t, verifyFactoryEnvelope(info, parseTestStack(t, path), path, false))
}

func TestParseConfigRejectsLocalReferenceInPin(t *testing.T) {
	path := stackEnvelopeFixture(t, "invalid/local-reference-in-pin")
	_, err := parseStackFile(path)
	require.Error(t, err)
	require.Contains(t, err.Error(),
		"`factory.pin.library-path:` must be a string literal")
}
