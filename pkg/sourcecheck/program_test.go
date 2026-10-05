package sourcecheck

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/codegen"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestAnalyzeProgramRetainsDataForSeparateGeneration(t *testing.T) {
	path := fixturePath("valid/import-analysis/factory/factory")
	body := parseFactoryAt(t, path)
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	require.Empty(t, errs)
	goSource := writeImportAnalysisGoLibrary(t)
	resolver := newTestResolver(t, filepath.Dir(path))
	resolver.remotes["example.com/schema"] = &resolve.Source{
		FS: os.DirFS(goSource), Path: goSource,
		ModulePath: "example.com/schema", GoImportPath: "example.com/schema",
	}
	reads := 0
	options := ImportAnalysisOptions{
		Resolver: resolver, Versions: map[string]string{"example.com/schema": "v1.0.0"},
		Source: sourceForDir(t, filepath.Dir(path)), Body: &body,
		SchemaCache: NewSchemaCacheWithReader(func(string) (
			*runtime.LibrarySchema, []string, error,
		) {
			reads++
			return importAnalysisSchema(), nil, nil
		}),
	}
	analysis, err := AnalyzeProgram(refs, options)
	require.NoError(t, err)
	require.Equal(t, 1, reads)
	require.Same(t, analysis.Libraries["wrap"], analysis.Libraries["wrap-again"])
	require.Equal(t, []string{"direct", "wrap"}, []string{
		analysis.UBLibraries[0].Name, analysis.UBLibraries[1].Name,
	})
	composite := analysis.UBLibraries[1].Composites[0]
	require.Equal(t, "resource", composite.Category)
	require.Equal(t, "wrapper", composite.Export)
	require.Equal(t, []string{"leaf", "std"}, []string{
		composite.Imports[0].LocalAlias, composite.Imports[1].LocalAlias,
	})
	require.NotEmpty(t, analysis.UBLibraries[1].SourceFiles)

	first, err := codegen.GenerateImports(analysis, "first")
	require.NoError(t, err)
	second, err := codegen.GenerateImports(analysis, "second")
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"direct": "first/internal/direct", "wrap": "first/internal/wrap",
		"wrap-again": "first/internal/wrap",
	}, first.UBImports)
	require.Equal(t, "second/internal/wrap", second.UBImports["wrap"])
	require.Contains(t, string(first.UBPackages["wrap"]), "first/internal/direct")
	require.Contains(t, string(second.UBPackages["wrap"]), "second/internal/direct")
	require.Equal(t, 1, reads)
	legacy, err := AnalyzeImports(refs, options)
	require.NoError(t, err)
	require.Empty(t, legacy.UBPackages)
	require.Empty(t, legacy.UBImports)
	options.StackName, options.GeneratePackages = "first", true
	legacy, err = AnalyzeImports(refs, options)
	require.NoError(t, err)
	require.Equal(t, first.UBPackages, legacy.UBPackages)
	require.Equal(t, first.UBImports, legacy.UBImports)
}

func TestAnalyzeProgramRetainsAssetsAfterGeneration(t *testing.T) {
	dir := fixtureDir(t, "valid/assets-analysis/factory")
	body := parseFactoryAtPath(t, filepath.Join(dir, "factory.ub"))
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	require.Empty(t, errs)
	analysis, err := AnalyzeProgram(refs, ImportAnalysisOptions{
		Resolver: newTestResolver(t, dir), Body: &body, Source: sourceForDir(t, dir),
		ValidateCompositeBodies: true,
		RootSourceFile: syntax.SourceFileSpec{
			ProjectRelPath: "factory.ub", PackageRelPath: "factory.ub",
		},
	})
	require.NoError(t, err)
	require.Len(t, analysis.UBLibraries, 1)
	composite := analysis.UBLibraries[0].Composites[0]
	declarations := slices.Clone(composite.Body.Assets)
	require.NotEmpty(t, declarations)
	require.NotEmpty(t, composite.AssetSetID)
	generated, err := codegen.GenerateImports(analysis, "assets")
	require.NoError(t, err)
	require.Contains(t, string(generated.UBPackages["bundle"]), composite.AssetSetID)
	require.NotContains(t, string(generated.UBPackages["bundle"]), "./payload.bin")
	require.Equal(t, declarations, analysis.UBLibraries[0].Composites[0].Body.Assets)
	require.Equal(t, declarations,
		analysis.Libraries["bundle"].Composite(runtime.NodeDataSource, "payload").SyntaxBody.Assets)
}
