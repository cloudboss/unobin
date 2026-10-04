package sourcecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestSchemaCacheChecksCompatibilityBeforeReaders(t *testing.T) {
	for _, configuration := range []bool{false, true} {
		t.Run(map[bool]string{false: "library", true: "configuration"}[configuration],
			func(t *testing.T) {
				dir := writeImportAnalysisGoLibrary(t)
				path := filepath.Join(dir, "library.go")
				body, err := os.ReadFile(path)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(
					string(body), `RequiredAPI: "1.0"`, `RequiredAPI: "1.1"`)), 0o644))
				calls := 0
				reader := func(string) (*runtime.LibrarySchema, []string, error) {
					calls++
					return &runtime.LibrarySchema{}, nil, nil
				}
				for _, cache := range []*SchemaCache{
					NewSchemaCache(), NewSchemaCacheWithReader(reader),
					NewSchemaCacheWithReaders(reader, reader),
				} {
					read := cache.Read
					if configuration {
						read = cache.ReadLibraryConfiguration
					}
					_, _, err := read(dir)
					var newer *libraryapi.NewerMinorError
					require.ErrorAs(t, err, &newer)
					ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
					require.Len(t, ds, 1)
					assert.Equal(t, "unobin.library-api.newer-minor", ds[0].Code)
					assert.Equal(t, path, ds[0].Path)
					require.NotNil(t, ds[0].Span)
				}
				assert.Zero(t, calls)
			})
	}
}

func TestSchemaCacheChecksCurrentMetadataBeforeCacheHits(t *testing.T) {
	for _, configuration := range []bool{false, true} {
		t.Run(map[bool]string{false: "library", true: "configuration"}[configuration],
			func(t *testing.T) {
				dir := writeImportAnalysisGoLibrary(t)
				calls := 0
				cache := NewSchemaCacheWithReader(
					func(string) (*runtime.LibrarySchema, []string, error) {
						calls++
						return importAnalysisSchema(), []string{"cached warning"}, nil
					})
				read := cache.Read
				if configuration {
					read = cache.ReadLibraryConfiguration
				}
				first, _, err := read(dir)
				require.NoError(t, err)
				again, warnings, err := read(dir)
				require.NoError(t, err)
				assert.Same(t, first, again)
				assert.Equal(t, []string{"cached warning"}, warnings)
				assert.Equal(t, 1, calls)

				path := filepath.Join(dir, "library.go")
				body, err := os.ReadFile(path)
				require.NoError(t, err)
				incompatible := strings.ReplaceAll(string(body), `"1.0"`, `"2.0"`)
				require.NoError(t, os.WriteFile(path, []byte(incompatible), 0o644))
				_, _, err = read(dir)
				var unsupported *libraryapi.UnsupportedMajorError
				require.ErrorAs(t, err, &unsupported)
				assert.Equal(t, 1, calls)
				require.NoError(t, os.WriteFile(path, body, 0o644))
				_, _, err = read(dir)
				require.NoError(t, err)
				assert.Equal(t, 2, calls)
			})
	}
}

func TestSchemaCacheUsesInjectedDescriptorForBothReaders(t *testing.T) {
	dir := writeImportAnalysisGoLibrary(t)
	path := filepath.Join(dir, "library.go")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(
		string(body), `"1.0"`, `"1.1"`)), 0o644))
	descriptor := libraryapi.Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.1"}, GeneratorAPI: "1.1",
	}
	context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
		Descriptor: &descriptor,
	})
	require.NoError(t, err)
	reader := func(string) (*runtime.LibrarySchema, []string, error) {
		return importAnalysisSchema(), nil, nil
	}
	for _, cache := range []*SchemaCache{
		NewSchemaCacheWithCompatibility(context),
		NewSchemaCacheWithReadersAndCompatibility(context, reader, reader),
	} {
		_, _, err := cache.Read(dir)
		require.NoError(t, err)
		_, _, err = cache.ReadLibraryConfiguration(dir)
		require.NoError(t, err)
		assert.Same(t, context, cache.CompatibilityContext())
	}
}

func TestSchemaCacheInvalidatesChangedSchemaSource(t *testing.T) {
	dir := writeImportAnalysisGoLibrary(t)
	cache := NewSchemaCache()
	first, _, err := cache.ReadLibraryConfiguration(dir)
	require.NoError(t, err)
	require.Equal(t, "region", first.ConfigurationFields[0].Name)
	path := filepath.Join(dir, "library.go")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(
		string(body), "Region string", "Zone string")), 0o644))
	after, _, err := cache.ReadLibraryConfiguration(dir)
	require.NoError(t, err)
	assert.NotSame(t, first, after)
	assert.Equal(t, "zone", after.ConfigurationFields[0].Name)
}

func TestSchemaCacheDefersUnavailablePaths(t *testing.T) {
	cache := NewSchemaCacheWithReader(
		func(string) (*runtime.LibrarySchema, []string, error) {
			t.Fatal("schema reader called for an unavailable source")
			return nil, nil, nil
		})
	for _, read := range []func(string) (*runtime.LibrarySchema, []string, error){
		cache.Read, cache.ReadLibraryConfiguration,
	} {
		schema, warnings, err := read("")
		assert.NoError(t, err)
		assert.Nil(t, schema)
		assert.Empty(t, warnings)
		_, _, err = read(filepath.Join(t.TempDir(), "missing"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestSchemaCacheKeepsSchemaOnlyCoreFloorOutsideBuild(t *testing.T) {
	dir := writeImportAnalysisGoLibrary(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(
		"module example.com/schema\n\ngo 1.26\n"+
			"\nrequire github.com/cloudboss/unobin v0.13.0\n"), 0o644))
	context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
		UnobinVersion: "v0.12.0",
	})
	require.NoError(t, err)
	cache := NewSchemaCacheWithCompatibility(context)
	_, _, err = cache.ReadLibraryConfiguration(dir)
	require.NoError(t, err)
	_, _, err = cache.Read(dir)
	var floor *golibrary.CoreFloorError
	require.ErrorAs(t, err, &floor)
}

func TestSchemaCacheRejectsSourceChangesDuringRead(t *testing.T) {
	for _, configuration := range []bool{false, true} {
		t.Run(map[bool]string{false: "library", true: "configuration"}[configuration],
			func(t *testing.T) {
				dir := writeImportAnalysisGoLibrary(t)
				path := filepath.Join(dir, "library.go")
				body, err := os.ReadFile(path)
				require.NoError(t, err)
				reader := func(string) (*runtime.LibrarySchema, []string, error) {
					require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(
						string(body), `"1.0"`, `"1.1"`)), 0o644))
					return importAnalysisSchema(), nil, nil
				}
				cache := NewSchemaCacheWithReaders(reader, reader)
				read := cache.Read
				if configuration {
					read = cache.ReadLibraryConfiguration
				}
				_, _, err = read(dir)
				var newer *libraryapi.NewerMinorError
				require.ErrorAs(t, err, &newer)
			})
	}
}

func TestSchemaCacheRejectsChangedCompatibleSourceDuringRead(t *testing.T) {
	for _, configuration := range []bool{false, true} {
		t.Run(map[bool]string{false: "library", true: "configuration"}[configuration],
			func(t *testing.T) {
				dir := writeImportAnalysisGoLibrary(t)
				path := filepath.Join(dir, "library.go")
				body, err := os.ReadFile(path)
				require.NoError(t, err)
				reader := func(string) (*runtime.LibrarySchema, []string, error) {
					require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(
						string(body), "Region string", "Zone string")), 0o644))
					return importAnalysisSchema(), nil, nil
				}
				cache := NewSchemaCacheWithReaders(reader, reader)
				read := cache.Read
				if configuration {
					read = cache.ReadLibraryConfiguration
				}
				_, _, err = read(dir)
				require.ErrorContains(t, err, "source changed while reading schema")
				_, _, err = read(dir)
				require.NoError(t, err)
			})
	}
}
