package sourcecheck

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestImportAnalysisRejectsEarlierLibraryEditsDuringLaterSchemaRead(t *testing.T) {
	first := writeImportAnalysisGoLibrary(t)
	second := writeImportAnalysisGoLibrary(t)
	require.NoError(t, os.WriteFile(filepath.Join(second, "go.mod"), []byte(
		"module example.com/second\n\ngo 1.26.2\n"), 0o644))
	resolver := newTestResolver(t, t.TempDir())
	for url, dir := range map[string]string{
		"example.com/schema": first, "example.com/second": second,
	} {
		resolver.remotes[url] = &resolve.Source{
			FS: os.DirFS(dir), Path: dir, ModulePath: url, GoImportPath: url,
		}
	}
	cache := NewSchemaCacheWithReader(func(dir string) (*runtime.LibrarySchema, []string, error) {
		if dir == second {
			path := filepath.Join(first, "library.go")
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, append(body, '\n'), 0o644))
		}
		return importAnalysisSchema(), nil, nil
	})
	analysis, err := AnalyzeImports(map[string]resolve.ImportRef{
		"a": &resolve.RemoteImport{URL: "example.com/schema"},
		"b": &resolve.RemoteImport{URL: "example.com/second"},
	}, ImportAnalysisOptions{
		Resolver: resolver, SchemaCache: cache,
		Versions: map[string]string{"example.com/schema": "v1.0.0", "example.com/second": "v1.0.0"},
	})
	require.Nil(t, analysis)
	require.ErrorContains(t, err, "source changed")
}

func TestImportAnalysisEndsSourceReuseBeforeLaterCacheReads(t *testing.T) {
	fixture := newAnalysisBenchmark(t, "valid/analysis-scaling/exports/count-10/factory")
	calls := 0
	dir := ""
	cache := NewSchemaCacheWithReader(func(source string) (*runtime.LibrarySchema, []string, error) {
		calls++
		dir = source
		return importAnalysisSchema(), nil, nil
	})
	fixture.options.SchemaCache = cache
	analysis, err := AnalyzeImports(fixture.refs, fixture.options)
	require.NoError(t, err)
	require.NotNil(t, analysis.Libraries["shared"].Composite(runtime.NodeResource, "box-009"))
	require.Equal(t, 1, calls)
	path := filepath.Join(dir, "library.go")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(body, '\n'), 0o644))
	_, _, err = cache.Read(dir)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}
