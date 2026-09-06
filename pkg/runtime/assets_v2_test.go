package runtime

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

func TestVersion2ResourceInputsKeepLogicalAssetsAcrossCaches(t *testing.T) {
	catalog, set := assetResolverCatalog(t)
	tree, err := set.Value("tree", "")
	require.NoError(t, err)
	archive, err := set.Value("archive", "")
	require.NoError(t, err)
	registration, err := newResourceDefinitionRegistration[
		assetBoundaryResource, *assetBoundaryOutput, *assetBoundaryConfig,
	](assetBoundaryResourceDefinition(&assetBoundaryRecorder{}), nil)
	require.NoError(t, err)
	firstRoot := filepath.Join(t.TempDir(), "plan")
	firstCache, err := asset.NewCache(catalog, firstRoot)
	require.NoError(t, err)
	logical, err := registration.forAssets(firstCache).prepareInputs(map[string]any{
		"label": "one", "replacement": "one", "path": tree.Path, "content": archive.Content,
		"nested":  []any{map[string]any{"path": tree.Path, "content": archive.Content}},
		"by-name": map[string]any{"archive": archive.Content},
	})
	require.NoError(t, err)
	fields, _ := logical.ObjectFields()
	require.Equal(t, StringValue(string(archive.Content)), fields["content"])
	require.Equal(t, StringValue(string(tree.Path)), fields["path"])
	for _, name := range []string{"read", "apply"} {
		root := filepath.Join(t.TempDir(), name)
		cache, err := asset.NewCache(catalog, root)
		require.NoError(t, err)
		resolved, err := resolveEncodedAssets(cache, logical)
		require.NoError(t, err)
		inputs, err := decodeResourceInputs[assetBoundaryResource](resolved)
		require.NoError(t, err)
		require.Contains(t, inputs.Path, root)
		require.NotContains(t, inputs.Path, firstRoot)
		require.Equal(t, []byte("zip bytes"), inputs.Content)
		require.Equal(t, []byte("zip bytes"), inputs.Nested[0].Content)
		require.Equal(t, []byte("zip bytes"), inputs.ByName["archive"])
	}
	unchanged, _ := logical.ObjectFields()
	require.Equal(t, fields, unchanged)
	_, err = resolveEncodedAssets(nil, logical)
	require.ErrorContains(t, err, "cache is not configured")
}

func TestFactoryV2ResolvesAssetsInInputsAndConfiguration(t *testing.T) {
	recorder := &assetBoundaryRecorder{}
	library := assetBoundaryLibrary(recorder)
	library.Configuration.(*cfg.ConfigurationType[*assetBoundaryConfig]).SchemaVersion = 1
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "github.com/example/native", New: func() *Library { return library }},
	})
	require.NoError(t, err)
	libraries, err := catalog.Libraries(map[string]string{"native": "github.com/example/native"})
	require.NoError(t, err)
	executor := &Executor{Libraries: libraries, LibraryCatalog: catalog, Store: newStateStore(t),
		Factory: newPlanEvaluationV2Snapshot(t).Factory}
	planRoot := filepath.Join(t.TempDir(), "plan")
	configureAssetBoundaryExecutor(t, executor, "executor", "one", planRoot)
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlanV2(*plan)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), planRoot)
	applyRoot := filepath.Join(t.TempDir(), "apply")
	setAssetBoundaryCache(t, executor, applyRoot)
	result, err := executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	require.Equal(t, "zip bytes", result.Outputs["inspected"])
	for _, method := range []string{"resource-create", "data-read", "action-run"} {
		assertAssetBoundaryRecordUnder(t, recorder, method, applyRoot)
	}
	refreshRoot := filepath.Join(t.TempDir(), "refresh")
	setAssetBoundaryCache(t, executor, refreshRoot)
	_, err = executor.RefreshV2(context.Background())
	require.NoError(t, err)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-read", refreshRoot)
}
