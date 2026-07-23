package sourcecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestImportAnalysisCapturesRootAndCompositeAssets(t *testing.T) {
	factoryDir := fixtureDir(t, "valid/assets-analysis/factory")
	factoryPath := filepath.Join(factoryDir, "factory.ub")
	body := parseFactoryAtPath(t, factoryPath)
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	require.Empty(t, errs)

	analysis, err := AnalyzeImports(refs, ImportAnalysisOptions{
		Resolver:                newTestResolver(t, factoryDir),
		StackName:               "assets",
		GeneratePackages:        true,
		ValidateCompositeBodies: true,
		Body:                    &body,
		Source:                  sourceForDir(t, factoryDir),
		RootSourceFile: syntax.SourceFileSpec{
			ProjectRelPath: "factory.ub",
			PackageRelPath: "factory.ub",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, analysis.Assets)
	require.NotEmpty(t, analysis.RootAssetSetID)
	require.Len(t, analysis.Assets.Catalog().Sets(), 2)

	rootSet, ok := analysis.Assets.Catalog().Set(analysis.RootAssetSetID)
	require.True(t, ok)
	rootAsset, ok := rootSet.Asset("root")
	require.True(t, ok)
	rootEntry, ok := rootAsset.Entry("")
	require.True(t, ok)
	require.Equal(t, int64(len("root asset\n")), rootEntry.ContentSize)

	composite := analysis.Libraries["bundle"].Composite(runtime.NodeDataSource, "payload")
	require.NotNil(t, composite)
	require.NotEmpty(t, composite.AssetSetID)
	compositeSet, ok := analysis.Assets.Catalog().Set(composite.AssetSetID)
	require.True(t, ok)
	content, ok := compositeSet.Asset("content")
	require.True(t, ok)
	contentEntry, ok := content.Entry("")
	require.True(t, ok)
	require.Equal(t, int64(len("composite asset\n")), contentEntry.ContentSize)

	require.Len(t, analysis.UBPackages, 1)
	for _, generated := range analysis.UBPackages {
		require.Contains(t, string(generated), "AssetSetID:")
		require.Contains(t, string(generated), composite.AssetSetID)
		require.NotContains(t, string(generated), "./payload.bin")
	}
}

func TestImportAnalysisRejectsMissingRootAsset(t *testing.T) {
	const name = "invalid/assets-analysis-missing/factory"
	path := fixturePath(name)
	body := parseFactoryAt(t, path)
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	require.Empty(t, errs)

	_, err := AnalyzeImports(refs, ImportAnalysisOptions{
		Body:   &body,
		Source: &resolve.Source{FS: os.DirFS(filepath.Dir(path))},
		RootSourceFile: syntax.SourceFileSpec{
			PackageRelPath: "factory.ub",
		},
	})
	require.Error(t, err)
	golden := ubtest.ReadFixture(t, fixturePath(name)+".err")
	require.Equal(
		t,
		strings.TrimSpace(golden),
		strings.TrimSpace(err.Error()),
	)
}

func TestImportAnalysisRejectsMissingCompositeAsset(t *testing.T) {
	const name = "invalid/assets-analysis-composite-missing/factory/factory"
	path := fixturePath(name)
	factoryDir := filepath.Dir(path)
	body := parseFactoryAt(t, path)
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	require.Empty(t, errs)

	_, err := AnalyzeImports(refs, ImportAnalysisOptions{
		Resolver: newTestResolver(t, factoryDir),
		Body:     &body,
		Source:   sourceForDir(t, factoryDir),
		RootSourceFile: syntax.SourceFileSpec{
			ProjectRelPath: "factory/factory.ub",
			PackageRelPath: "factory.ub",
		},
	})
	require.Error(t, err)
	golden := ubtest.ReadFixture(t, fixturePath(name)+".err")
	require.Equal(
		t,
		strings.TrimSpace(golden),
		strings.TrimSpace(err.Error()),
	)
}
