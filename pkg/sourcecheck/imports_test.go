package sourcecheck

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/require"
)

func TestImportAnalysisBuildsSameLibrariesForCompileAndGraph(t *testing.T) {
	path := fixturePath("valid/import-analysis/factory/factory")
	body := parseFactoryAt(t, path)
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	require.Empty(t, errs)

	goSource := writeImportAnalysisGoLibrary(t)
	resolver := newTestResolver(t, filepath.Dir(path))
	resolver.remotes["example.com/schema"] = &resolve.Source{
		FS:           os.DirFS(goSource),
		Path:         goSource,
		ModulePath:   "example.com/schema",
		GoImportPath: "example.com/schema",
	}

	reads := map[string]int{}
	schemas := NewSchemaCacheWithReader(
		func(sourcePath string) (*runtime.LibrarySchema, []string, error) {
			reads[sourcePath]++
			return importAnalysisSchema(), nil, nil
		},
	)

	analysis, err := AnalyzeImports(refs, ImportAnalysisOptions{
		Resolver: resolver,
		Versions: map[string]string{
			"example.com/schema": "v1.0.0",
		},
		SchemaCache:      schemas,
		StackName:        "factory",
		GeneratePackages: true,
		Source: &resolve.Source{
			FS:   os.DirFS(filepath.Dir(path)),
			Path: filepath.Dir(path),
		},
	})
	require.NoError(t, err)

	require.Contains(t, analysis.Libraries, "direct")
	require.Contains(t, analysis.Libraries, "std")
	require.Contains(t, analysis.Libraries, "wrap")
	require.Contains(t, analysis.Libraries, "wrap-again")
	require.Same(t, analysis.Libraries["wrap"], analysis.Libraries["wrap-again"])
	require.NotSame(t, analysis.Libraries["direct"], analysis.Libraries["wrap"])

	std := analysis.Libraries["std"]
	require.NotNil(t, std.Schema)
	require.Contains(t, std.Schema.Resources, "file")
	require.Equal(t, map[string]int{goSource: 1}, reads)

	wrapper := analysis.Libraries["wrap"].Composite(runtime.NodeResource, "wrapper")
	require.NotNil(t, wrapper)
	require.Contains(t, wrapper.Libraries, "leaf")
	require.Contains(t, wrapper.Libraries, "std")

	leaf := wrapper.Libraries["leaf"].Composite(runtime.NodeResource, "leaf")
	require.NotNil(t, leaf)
	require.Contains(t, leaf.Libraries, "std")

	require.Equal(t, map[string]string{
		"std": "example.com/schema",
	}, analysis.GoImports)
	require.Equal(t, map[string]string{
		"example.com/schema": "v1.0.0",
	}, analysis.GoModules)
	require.Len(t, analysis.UBPackages, 2)
	require.Equal(t, map[string]string{
		"direct":     "factory/internal/direct",
		"wrap":       "factory/internal/wrap",
		"wrap-again": "factory/internal/wrap",
	}, analysis.UBImports)
}

func BenchmarkImportAnalysisNestedUBLibraries(b *testing.B) {
	root := b.TempDir()
	factoryDir := filepath.Join(root, "factory")
	innerDir := filepath.Join(root, "inner")
	outerDir := filepath.Join(root, "outer")
	goDir := filepath.Join(root, "go")
	for _, dir := range []string{factoryDir, innerDir, outerDir, goDir} {
		require.NoError(b, os.MkdirAll(dir, 0o755))
	}
	require.NoError(b, os.WriteFile(filepath.Join(goDir, "library.go"),
		[]byte("package schema\n"), 0o644))
	copyImportAnalysisFixture(b,
		"valid/benchmark-import-analysis/factory/factory.ub",
		filepath.Join(factoryDir, "factory.ub"))
	copyImportAnalysisFixture(b,
		"valid/benchmark-import-analysis/inner/library.ub",
		filepath.Join(innerDir, "library.ub"))
	copyImportAnalysisFixture(b,
		"valid/benchmark-import-analysis/outer/library.ub",
		filepath.Join(outerDir, "library.ub"))

	body := parseFactoryAtPath(b, filepath.Join(factoryDir, "factory.ub"))
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	require.Empty(b, errs)
	resolver := newTestResolver(b, factoryDir)
	resolver.remotes["example.com/schema"] = &resolve.Source{
		FS:           os.DirFS(goDir),
		Path:         goDir,
		ModulePath:   "example.com/schema",
		GoImportPath: "example.com/schema",
	}
	opts := ImportAnalysisOptions{
		Resolver: resolver,
		Versions: map[string]string{
			"example.com/schema": "v1.0.0",
		},
		SchemaCache: NewSchemaCacheWithReader(
			func(string) (*runtime.LibrarySchema, []string, error) {
				return importAnalysisSchema(), nil, nil
			},
		),
		StackName:        "factory",
		GeneratePackages: true,
		Source: &resolve.Source{
			FS:   os.DirFS(factoryDir),
			Path: factoryDir,
		},
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := AnalyzeImports(refs, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func parseFactoryAtPath(t testing.TB, path string) syntax.FactoryBody {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	file, err := syntax.ParseSource(path, body)
	require.NoError(t, err)
	require.NotNil(t, file.Factory)
	if errs := syntax.ValidateFile(file); errs.Len() > 0 {
		t.Fatalf("validate %s: %v", path, errs.Err())
	}
	return file.Factory.Body
}

func writeImportAnalysisGoLibrary(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "library.go"),
		[]byte("package schema\n"), 0o644))
	return dir
}

func copyImportAnalysisFixture(t testing.TB, name, dst string) {
	t.Helper()
	src := filepath.Join(sourcecheckFixtureRoot(), filepath.FromSlash(name))
	body, err := os.ReadFile(src)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, body, 0o644))
}

func importAnalysisSchema() *runtime.LibrarySchema {
	stringType := typecheck.TString()
	return &runtime.LibrarySchema{
		Resources: map[string]*runtime.TypeSchema{
			"file": {
				Inputs: map[string]typecheck.Type{
					"content": stringType,
					"path":    stringType,
				},
				Outputs: map[string]typecheck.Type{
					"path": stringType,
				},
			},
		},
	}
}
