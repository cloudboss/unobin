package e2etest

import (
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2ELibraryFixtureSchema(t *testing.T) {
	schema, warnings, err := goschema.Read(e2eLibraryDir(t))
	require.NoError(t, err)
	assert.Empty(t, warnings)

	require.Contains(t, schema.Resources, "file")
	require.Contains(t, schema.Resources, "object")
	require.Contains(t, schema.DataSources, "read-file")
	require.Contains(t, schema.Actions, "echo")
	require.Contains(t, schema.Actions, "record")

	file := schema.Resources["file"]
	assert.Contains(t, file.Inputs, "path")
	assert.Contains(t, file.Inputs, "content")
	assert.Contains(t, file.Outputs, "sha256")
	assert.NotEmpty(t, file.Defaults)
	assert.NotEmpty(t, file.Constraints)

	object := schema.Resources["object"]
	assert.Contains(t, object.Inputs, "body")
	assert.Contains(t, object.Outputs, "body")
	assert.NotEmpty(t, object.Constraints)

	readFile := schema.DataSources["read-file"]
	assert.Contains(t, readFile.Outputs, "content")
	assert.Contains(t, readFile.Outputs, "size")

	record := schema.Actions["record"]
	assert.Contains(t, record.Outputs, "record")

	require.True(t, schema.HasConfiguration)
	assert.Contains(t, schema.Configuration, "base-dir")
	assert.Contains(t, schema.Configuration, "event-log-path")
	assert.Contains(t, schema.Configuration, "prefix")
	assert.Contains(t, schema.Configuration, "nested")
	assert.NotEmpty(t, schema.ConfigurationDefaults)

	join := schema.Functions["join"]
	require.Len(t, join.Params, 1)
	require.NotNil(t, join.Variadic)
	assert.Equal(t, "string", join.Result.String())
	assert.Contains(t, schema.Functions, "all")
	assert.Contains(t, schema.Functions, "length")
	assert.Contains(t, schema.Functions, "project")
	assert.Contains(t, schema.Functions, "fail")
}

func e2eLibraryDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(e2eRepoRoot(t), "tests", "e2e", "testdata", "modules", "e2elib")
}

func e2eRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := goruntime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
