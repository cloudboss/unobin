package goschema

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadWithIndexReturnsSchemaEqualToRead(t *testing.T) {
	schema, warnings, err := Read("testdata/definition")
	require.NoError(t, err)

	indexedSchema, index, indexedWarnings, err := ReadWithIndex("testdata/definition")
	require.NoError(t, err)
	require.Equal(t, warnings, indexedWarnings)
	require.Equal(t, schema, indexedSchema)
	require.NotNil(t, index)
}

func TestReadWithIndexReturnsSourceLocations(t *testing.T) {
	_, index, _, err := ReadWithIndex("testdata/definition")
	require.NoError(t, err)

	requireLocationPrefix(t, index.LibraryFunc, "library.go", "Library")
	requireLocationPrefix(t, index.Registrations["resource"]["server"], "library.go", "\"server\"")
	requireLocationPrefix(t, index.Registrations["data-source"]["lookup"], "library.go", "\"lookup\"")
	requireLocationPrefix(t, index.Registrations["action"]["deploy"], "library.go", "\"deploy\"")
	requireLocationPrefix(t, index.InputTypes["resource"]["server"], "library.go", "Server")
	requireLocationPrefix(t, index.OutputTypes["resource"]["server"], "library.go", "ServerOutput")
	requireLocationPrefix(t, index.InputFields["resource"]["server"]["server-name"],
		"library.go", "Name")
	requireLocationPrefix(t, index.OutputFields["resource"]["server"]["endpoint.url"],
		"shared.go", "URL")
	requireLocationPrefix(t, index.ConfigType, "library.go", "Config")
	requireLocationPrefix(t, index.ConfigFields["region"], "library.go", "Region")
	requireLocationPrefix(t, index.ConfigFields["retry.count"], "shared.go", "Count")
	requireLocationPrefix(t, index.Functions["slug"], "library.go", "makeSlug")
}

func TestReadWithIndexCrossPackageFieldPaths(t *testing.T) {
	_, index, _, err := ReadWithIndex("testdata/definition")
	require.NoError(t, err)

	loc := index.InputFields["resource"]["server"]["settings.endpoint.url"]
	requireLocationPrefix(t, loc, "shared.go", "URL")
}

func TestSourceIndexCacheInvalidatesChangedSource(t *testing.T) {
	dir := copyDefinitionFixture(t)
	cache := NewSourceIndexCache()
	_, first, _, err := cache.Read(dir)
	require.NoError(t, err)
	require.Contains(t, first.OutputFields["resource"]["server"], "id")

	path := filepath.Join(dir, "library.go")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	updated := strings.Replace(string(body),
		"type ServerOutput struct {\n\tID       string\n",
		"type ServerOutput struct {\n\tIdentifier string\n", 1)
	require.NotEqual(t, string(body), updated)
	require.NoError(t, os.WriteFile(path, []byte(updated), 0o644))

	_, cached, _, err := cache.Read(dir)
	require.NoError(t, err)
	require.Contains(t, cached.OutputFields["resource"]["server"], "id")
	require.NotContains(t, cached.OutputFields["resource"]["server"], "identifier")

	cache.Invalidate(dir)
	_, fresh, _, err := cache.Read(dir)
	require.NoError(t, err)
	require.NotContains(t, fresh.OutputFields["resource"]["server"], "id")
	require.Contains(t, fresh.OutputFields["resource"]["server"], "identifier")
}

func requireLocationPrefix(t *testing.T, loc GoLocation, fileBase string, prefix string) {
	t.Helper()
	require.NotEmpty(t, loc.Path)
	require.Equal(t, fileBase, filepath.Base(loc.Path))
	require.Greater(t, loc.Line, 0)
	require.Greater(t, loc.Column, 0)
	require.GreaterOrEqual(t, loc.Offset, 0)
	body, err := os.ReadFile(loc.Path)
	require.NoError(t, err)
	require.Less(t, loc.Offset, len(body))
	require.True(t, strings.HasPrefix(string(body[loc.Offset:]), prefix),
		"location %s:%d:%d does not point at %q", loc.Path, loc.Line, loc.Column, prefix)
}

func copyDefinitionFixture(t *testing.T) string {
	t.Helper()
	srcRoot := filepath.Join("testdata", "definition")
	dstRoot := filepath.Join(t.TempDir(), "definition")
	err := filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, body, 0o644)
	})
	require.NoError(t, err)
	return dstRoot
}
