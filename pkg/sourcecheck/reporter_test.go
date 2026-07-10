package sourcecheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

type reporterGolden struct {
	Error          string                  `json:"error"`
	Diagnostics    []diagnostic.Diagnostic `json:"diagnostics"`
	ImportError    []diagnostic.Diagnostic `json:"import-error"`
	CompositeError []diagnostic.Diagnostic `json:"composite-error"`
}

func TestImportAnalysisReporterGolden(t *testing.T) {
	path := fixturePath("valid/schema-dependencies/aws-split/factory")
	body := parseFactoryAt(t, path)
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	require.Empty(t, errs)

	servicePath := writeImportAnalysisGoLibrary(t)
	configPath := writeImportAnalysisGoLibrary(t)
	resolver := newTestResolver(t, filepath.Dir(path))
	resolver.remotes["example.com/aws//service"] = &resolve.Source{
		FS: os.DirFS(servicePath), Path: servicePath,
		ModulePath: "example.com/aws", GoImportPath: "example.com/aws/service",
	}
	resolver.remotes["example.com/aws//config"] = &resolve.Source{
		FS: os.DirFS(configPath), Path: configPath,
		ModulePath: "example.com/aws", GoImportPath: "example.com/aws/config",
	}
	cache := NewSchemaCacheWithReaders(
		func(string) (*runtime.LibrarySchema, []string, error) {
			return importAnalysisSchema(), []string{"resource output type unavailable"}, nil
		},
		func(string) (*runtime.LibrarySchema, []string, error) {
			return importAnalysisSchema(), []string{"configuration default unavailable"}, nil
		},
	)
	collector := &diagnostic.Collector{}
	_, err := AnalyzeImports(refs, ImportAnalysisOptions{
		Resolver:    resolver,
		Versions:    map[string]string{"example.com/aws": "v1.0.0"},
		SchemaCache: cache,
		Source:      &resolve.Source{FS: os.DirFS(filepath.Dir(path)), Path: filepath.Dir(path)},
		Body:        &body,
		Reporter:    collector,
	})
	result := reporterGolden{
		Error:       reporterErrorString(err),
		Diagnostics: collector.Diagnostics(),
	}
	failingCache := NewSchemaCacheWithReader(
		func(string) (*runtime.LibrarySchema, []string, error) {
			return nil, nil, &parse.Error{Kind: parse.ErrSchema, Msg: "schema unavailable"}
		},
	)
	_, err = AnalyzeImports(refs, ImportAnalysisOptions{
		Resolver:    resolver,
		Versions:    map[string]string{"example.com/aws": "v1.0.0"},
		SchemaCache: failingCache,
		Source:      &resolve.Source{FS: os.DirFS(filepath.Dir(path)), Path: filepath.Dir(path)},
		Body:        &body,
	})
	result.ImportError = diagnostic.FromError(err, diagnostic.ConvertOptions{})
	compositeRoot := fixtureDir(t, "valid/import-analysis/wrap")
	compositeResolver := newTestResolver(t, compositeRoot)
	compositeResolver.remotes["example.com/schema"] = &resolve.Source{
		FS: os.DirFS(servicePath), Path: servicePath,
		ModulePath: "example.com/schema", GoImportPath: "example.com/schema",
	}
	err = CheckUBLibrary(sourceForDir(t, compositeRoot), Options{
		Resolver:    compositeResolver,
		Versions:    map[string]string{"example.com/schema": "v1.0.0"},
		SchemaCache: failingCache,
	})
	result.CompositeError = diagnostic.FromError(err, diagnostic.ConvertOptions{})
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/reporter.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func reporterErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
