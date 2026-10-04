package compile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
)

func TestSchemaCacheReadsEachPathOnce(t *testing.T) {
	root := t.TempDir()
	for _, library := range []string{"disk", "net"} {
		dir := filepath.Join(root, library)
		require.NoError(t, os.Mkdir(dir, 0o755))
		for _, name := range []string{"go.mod", "library.go"} {
			body, err := os.ReadFile(filepath.Join(
				"..", "sourcecheck", "testdata", "golibrary", "schema", name))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), body, 0o644))
		}
	}
	disk, network := filepath.Join(root, "disk"), filepath.Join(root, "net")
	var calls []string
	c := NewSchemaCacheWithReader(
		func(sourcePath string) (*ubruntime.LibrarySchema, []string, error) {
			calls = append(calls, sourcePath)
			return &ubruntime.LibrarySchema{}, []string{"warning for " + sourcePath}, nil
		},
	)

	first, warnings, err := c.Read(disk)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, []string{"warning for " + disk}, warnings)

	again, warnings, err := c.Read(disk)
	require.NoError(t, err)
	require.Same(t, first, again)
	require.Equal(t, []string{"warning for " + disk}, warnings)

	_, _, err = c.Read(network)
	require.NoError(t, err)
	require.Equal(t, []string{disk, network}, calls)
}

func TestSchemaCacheDoesNotStoreFailures(t *testing.T) {
	dir := filepath.Join("..", "sourcecheck", "testdata", "golibrary", "schema")
	readFailed := errors.New("read failed")
	calls := 0
	c := NewSchemaCacheWithReader(
		func(string) (*ubruntime.LibrarySchema, []string, error) {
			calls++
			return nil, nil, readFailed
		},
	)

	_, _, err := c.Read(dir)
	require.ErrorIs(t, err, readFailed)
	_, _, err = c.Read(dir)
	require.ErrorIs(t, err, readFailed)
	require.Equal(t, 2, calls)
}

func TestCompileSchemaReadersUseOneCompatibilityDescriptor(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"go.mod", "library.go"} {
		body, err := os.ReadFile(filepath.Join(
			"..", "sourcecheck", "testdata", "golibrary", "schema", name))
		require.NoError(t, err)
		updated := strings.ReplaceAll(string(body), `RequiredAPI: "1.0"`, `RequiredAPI: "1.1"`)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(updated), 0o644))
	}
	cache := NewSchemaCache()
	standalone := func(sourcePath string) (*ubruntime.LibrarySchema, []string, error) {
		return ReadGoSchema(sourcePath)
	}
	for _, read := range []func(string) (*ubruntime.LibrarySchema, []string, error){
		standalone, cache.Read, cache.ReadLibraryConfiguration,
	} {
		_, _, err := read(dir)
		var newer *libraryapi.NewerMinorError
		require.ErrorAs(t, err, &newer)
	}
	descriptor := libraryapi.Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.1"}, GeneratorAPI: "1.1",
	}
	context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
		Descriptor: &descriptor,
	})
	require.NoError(t, err)
	_, _, err = ReadGoSchemaWithCompatibility(dir, context)
	require.NoError(t, err)
	cache = NewSchemaCacheWithCompatibility(context)
	_, _, err = cache.Read(dir)
	require.NoError(t, err)
	_, _, err = cache.ReadLibraryConfiguration(dir)
	require.NoError(t, err)
}
