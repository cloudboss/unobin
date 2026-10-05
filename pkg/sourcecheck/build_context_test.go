package sourcecheck

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestSchemaCacheInvalidatesChangedGoTarget(t *testing.T) {
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "amd64")
	t.Setenv("CGO_ENABLED", "0")
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOAMD64", "v1")
	dir := filepath.Join("..", "gopackage", "testdata", "buildcontext")
	reads := 0
	cache := NewSchemaCacheWithReader(func(dir string) (*runtime.LibrarySchema, []string, error) {
		reads++
		return goschema.Read(dir)
	})
	first, warnings, err := cache.Read(dir)
	require.NoError(t, err)
	require.Empty(t, warnings)
	unchanged, _, err := cache.Read(dir)
	require.NoError(t, err)
	require.Same(t, first, unchanged)
	require.Equal(t, 1, reads)
	t.Setenv("GOAMD64", "v2")
	second, warnings, err := cache.Read(dir)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, 2, reads)
	require.Equal(t, "low", first.DataSources["query"].Inputs["tuning"].Fields[0].Name)
	require.Equal(t, "high", second.DataSources["query"].Inputs["tuning"].Fields[0].Name)
}

func TestSchemaCacheUsesDeclaredConfigurationImportName(t *testing.T) {
	dir := filepath.Join("..", "goschema", "testdata", "fileimports")
	cache := NewSchemaCache()
	schema, warnings, err := cache.Read(dir)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "example.com/fileimports/config.Config", schema.ConfigurationIdentity)
	require.Equal(t, []string{"secret"}, schema.DataSources["local"].SensitiveOutputs)
}
