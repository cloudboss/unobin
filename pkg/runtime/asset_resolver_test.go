package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

const assetBoundaryFixtureDir = "testdata/ub/asset-boundaries"

type resolvedAssetInputs struct {
	Path    string                `ub:"path"`
	Content []byte                `ub:"content"`
	Nested  []resolvedAssetNested `ub:"nested"`
	ByName  map[string][]byte     `ub:"by-name"`
	Options map[string]any        `ub:"options"`
}

type resolvedAssetNested struct {
	Path    string `ub:"path"`
	Content []byte `ub:"content"`
}

func TestResolveAssetValueCopiesAndResolvesNestedReferences(t *testing.T) {
	catalog, set := assetResolverCatalog(t)
	tree, err := set.Value("tree", "")
	require.NoError(t, err)
	archive, err := set.Value("archive", "")
	require.NoError(t, err)
	cache, err := asset.NewCache(catalog, filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)
	logical := map[string]any{
		"path":    tree.Path,
		"content": archive.Content,
		"nested": []any{
			map[string]any{
				"path":    tree.Path,
				"content": archive.Content,
			},
		},
		"by-name": map[string]any{"archive": archive.Content},
		"options": map[string]any{"ordinary": "unchanged"},
	}

	resolved, err := resolveAssetValue(cache, logical)
	require.NoError(t, err)
	got := resolved.(map[string]any)

	require.IsType(t, "", got["path"])
	require.DirExists(t, got["path"].(string))
	require.Equal(t, []byte("zip bytes"), got["content"])
	require.Equal(
		t,
		[]byte("zip bytes"),
		got["nested"].([]any)[0].(map[string]any)["content"],
	)
	require.Equal(
		t,
		[]byte("zip bytes"),
		got["by-name"].(map[string]any)["archive"],
	)
	require.Equal(
		t,
		"unchanged",
		got["options"].(map[string]any)["ordinary"],
	)
	require.Equal(t, tree.Path, logical["path"])
	require.Equal(t, archive.Content, logical["content"])
	require.IsType(t, asset.PathRef(""), logical["path"])
	require.IsType(t, asset.ContentRef(""), logical["content"])
}

func TestResolveAssetValueCopiesByteSlices(t *testing.T) {
	original := []byte("ordinary bytes")

	resolved, err := resolveAssetValue(nil, original)
	require.NoError(t, err)
	got := resolved.([]byte)

	require.Equal(t, original, got)
	got[0] = 'X'
	require.Equal(t, []byte("ordinary bytes"), original)
}

func TestResolveAssetValueUsesHistoricalCache(t *testing.T) {
	catalog, set := assetResolverCatalog(t)
	archive, err := set.Value("archive", "")
	require.NoError(t, err)
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	current, err := asset.NewCache(catalog, cacheRoot)
	require.NoError(t, err)
	_, err = current.Resolve(string(archive.Content))
	require.NoError(t, err)
	historical, err := asset.NewCache(nil, cacheRoot)
	require.NoError(t, err)

	resolved, err := resolveAssetValue(historical, string(archive.Content))
	require.NoError(t, err)
	require.Equal(t, []byte("zip bytes"), resolved)
}

func TestResolveAssetValueRejectsReferenceWithoutCache(t *testing.T) {
	_, set := assetResolverCatalog(t)
	archive, err := set.Value("archive", "")
	require.NoError(t, err)

	_, err = resolveAssetValue(nil, archive.Content)
	require.EqualError(t, err, "asset <asset.archive.content>: cache is not configured")
}

func TestExecutorDecodeInputsResolvesAssetsWithoutChangingLogicalMap(t *testing.T) {
	catalog, set := assetResolverCatalog(t)
	tree, err := set.Value("tree", "")
	require.NoError(t, err)
	archive, err := set.Value("archive", "")
	require.NoError(t, err)
	cache, err := asset.NewCache(catalog, filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)
	exec := &Executor{AssetCache: cache}
	logical := map[string]any{
		"path":    tree.Path,
		"content": archive.Content,
		"nested": []any{
			map[string]any{
				"path":    tree.Path,
				"content": archive.Content,
			},
		},
		"by-name": map[string]any{"archive": archive.Content},
		"options": map[string]any{"logical": archive.Content},
	}
	var receiver resolvedAssetInputs

	err = exec.decodeInputs(&receiver, logical)
	require.NoError(t, err)

	require.DirExists(t, receiver.Path)
	require.Equal(t, []byte("zip bytes"), receiver.Content)
	require.Len(t, receiver.Nested, 1)
	require.DirExists(t, receiver.Nested[0].Path)
	require.Equal(t, []byte("zip bytes"), receiver.Nested[0].Content)
	require.Equal(t, []byte("zip bytes"), receiver.ByName["archive"])
	require.Equal(t, []byte("zip bytes"), receiver.Options["logical"])
	require.Equal(t, tree.Path, logical["path"])
	require.Equal(t, archive.Content, logical["content"])
}

func TestEvalImportedFunctionResolvesAssetArguments(t *testing.T) {
	body := assetBoundaryFactoryFixture(t, true)
	catalog, set := assetEvalCatalog(t, body, assetEvalFS("shared\n"))
	cache, err := asset.NewCache(catalog, filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)
	ctx := &EvalContext{
		Assets:     set,
		AssetCache: cache,
		Libraries: map[string]*Library{
			"native": {
				Functions: map[string]FunctionType{
					"inspect": MakeFunc(
						"inspect",
						"Return the content as text.",
						func(content []byte) (string, error) {
							return string(content), nil
						},
					),
				},
			},
		},
	}

	got, err := Eval(assetEvalLocal(t, body, "result"), ctx)
	require.NoError(t, err)
	require.Equal(t, "zip bytes", got)
}

func TestEvalImportedFunctionRejectsMissingAssetCache(t *testing.T) {
	body := assetBoundaryFactoryFixture(t, false)
	_, set := assetEvalCatalog(t, body, assetEvalFS("shared\n"))
	ctx := &EvalContext{
		Assets: set,
		Libraries: map[string]*Library{
			"native": {
				Functions: map[string]FunctionType{
					"inspect": MakeFunc(
						"inspect",
						"Return the content as text.",
						func(content []byte) (string, error) {
							return string(content), nil
						},
					),
				},
			},
		},
	}

	_, err := Eval(assetEvalLocal(t, body, "result"), ctx)
	require.EqualError(
		t,
		err,
		"eval: native.inspect arg 0: asset <asset.archive.content>: "+
			"cache is not configured",
	)
}

type assetBoundaryRecorder struct {
	mu      sync.Mutex
	records map[string][]string
}

func (r *assetBoundaryRecorder) add(method, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.records == nil {
		r.records = map[string][]string{}
	}
	r.records[method] = append(r.records[method], path)
}

func (r *assetBoundaryRecorder) paths(method string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.records[method]...)
}

type assetBoundaryConfig struct {
	Path    string `ub:"path"`
	Content []byte `ub:"content"`
}

type assetBoundaryOutput struct {
	ID string `ub:"id"`
}

type assetBoundaryResource struct {
	Label       string                `ub:"label"`
	Replacement string                `ub:"replacement"`
	Path        string                `ub:"path"`
	Content     []byte                `ub:"content"`
	Nested      []resolvedAssetNested `ub:"nested"`
	ByName      map[string][]byte     `ub:"by-name"`
	recorder    *assetBoundaryRecorder
}

func (r *assetBoundaryResource) SchemaVersion() int {
	return 1
}

func (r *assetBoundaryResource) Create(
	_ context.Context,
	config *assetBoundaryConfig,
) (*assetBoundaryOutput, error) {
	if err := verifyAssetBoundaryValues(r.Path, r.Content, config); err != nil {
		return nil, err
	}
	if len(r.Nested) != 1 ||
		r.Nested[0].Path != r.Path ||
		string(r.Nested[0].Content) != "zip bytes" ||
		string(r.ByName["archive"]) != "zip bytes" {
		return nil, fmt.Errorf("resource: nested values were not resolved")
	}
	r.recorder.add("resource-create", r.Path)
	return &assetBoundaryOutput{ID: "resource-id"}, nil
}

func (r *assetBoundaryResource) Read(
	_ context.Context,
	config *assetBoundaryConfig,
	prior *assetBoundaryOutput,
) (*assetBoundaryOutput, error) {
	if err := verifyAssetBoundaryValues(r.Path, r.Content, config); err != nil {
		return nil, err
	}
	r.recorder.add("resource-read", r.Path)
	return prior, nil
}

func (r *assetBoundaryResource) Update(
	_ context.Context,
	config *assetBoundaryConfig,
	prior Prior[assetBoundaryResource, *assetBoundaryOutput],
) (*assetBoundaryOutput, error) {
	if err := verifyAssetBoundaryValues(r.Path, r.Content, config); err != nil {
		return nil, err
	}
	if err := verifyAssetBoundaryInputs(prior.Inputs.Path, prior.Inputs.Content); err != nil {
		return nil, fmt.Errorf("prior inputs: %w", err)
	}
	r.recorder.add("resource-update", r.Path)
	r.recorder.add("resource-update-prior", prior.Inputs.Path)
	return &assetBoundaryOutput{ID: "resource-id"}, nil
}

func (r *assetBoundaryResource) Delete(
	_ context.Context,
	config *assetBoundaryConfig,
	_ *assetBoundaryOutput,
) error {
	if err := verifyAssetBoundaryValues(r.Path, r.Content, config); err != nil {
		return err
	}
	r.recorder.add("resource-delete", r.Path)
	return nil
}

func (r *assetBoundaryResource) ReplaceFields() []string {
	return []string{"replacement"}
}

func (r *assetBoundaryResource) ValidateInputs(
	_ context.Context,
	config *assetBoundaryConfig,
) error {
	if err := verifyAssetBoundaryValues(r.Path, r.Content, config); err != nil {
		return err
	}
	r.recorder.add("resource-validate", r.Path)
	return nil
}

func (r *assetBoundaryResource) EquivalentInput(
	_ string,
	prior, _ assetBoundaryResource,
) bool {
	if err := verifyAssetBoundaryInputs(prior.Path, prior.Content); err != nil {
		r.recorder.add("resource-equivalent-invalid", err.Error())
	} else {
		r.recorder.add("resource-equivalent", prior.Path)
	}
	return false
}

func (r *assetBoundaryResource) ModifyResourcePlan(
	req ResourcePlanRequest[
		assetBoundaryResource,
		*assetBoundaryOutput,
		*assetBoundaryConfig,
	],
	_ *ResourcePlanResponse,
) error {
	if err := verifyAssetBoundaryValues(r.Path, r.Content, req.Config); err != nil {
		return err
	}
	if err := verifyAssetBoundaryInputs(
		req.PriorInputs.Path,
		req.PriorInputs.Content,
	); err != nil {
		return fmt.Errorf("prior inputs: %w", err)
	}
	r.recorder.add("resource-modify-plan", r.Path)
	r.recorder.add("resource-modify-plan-prior", req.PriorInputs.Path)
	return nil
}

type assetBoundaryData struct {
	Path     string `ub:"path"`
	Content  []byte `ub:"content"`
	recorder *assetBoundaryRecorder
}

func (d *assetBoundaryData) Read(
	_ context.Context,
	config *assetBoundaryConfig,
) (*assetBoundaryOutput, error) {
	if err := verifyAssetBoundaryValues(d.Path, d.Content, config); err != nil {
		return nil, err
	}
	d.recorder.add("data-read", d.Path)
	return &assetBoundaryOutput{ID: "data-id"}, nil
}

type assetBoundaryAction struct {
	Path     string `ub:"path"`
	Content  []byte `ub:"content"`
	recorder *assetBoundaryRecorder
}

func (a *assetBoundaryAction) Run(
	_ context.Context,
	config *assetBoundaryConfig,
) (*assetBoundaryOutput, error) {
	if err := verifyAssetBoundaryValues(a.Path, a.Content, config); err != nil {
		return nil, err
	}
	a.recorder.add("action-run", a.Path)
	return &assetBoundaryOutput{ID: "action-id"}, nil
}

func verifyAssetBoundaryValues(
	path string,
	content []byte,
	config *assetBoundaryConfig,
) error {
	if err := verifyAssetBoundaryInputs(path, content); err != nil {
		return err
	}
	if config == nil {
		return fmt.Errorf("configuration was not resolved")
	}
	if err := verifyAssetBoundaryInputs(config.Path, config.Content); err != nil {
		return fmt.Errorf("configuration was not resolved: %w", err)
	}
	return nil
}

func verifyAssetBoundaryInputs(path string, content []byte) error {
	if strings.HasPrefix(path, "unobin-asset:") {
		return fmt.Errorf("path remained logical")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("path was not materialized: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory")
	}
	if string(content) != "zip bytes" {
		return fmt.Errorf("content was not resolved")
	}
	return nil
}

func TestExecutorResolvesEveryGoBoundaryAndKeepsLogicalState(t *testing.T) {
	src := ubtest.ReadValidFixture(t, assetBoundaryFixtureDir, "executor")
	body := parseSyntaxFactoryFixture(t, src).body
	catalog, set := assetEvalCatalog(t, body, assetEvalFS("shared\n"))
	firstRoot := filepath.Join(t.TempDir(), "plan-cache")
	secondRoot := filepath.Join(t.TempDir(), "apply-cache")
	firstCache, err := asset.NewCache(catalog, firstRoot)
	require.NoError(t, err)
	secondCache, err := asset.NewCache(catalog, secondRoot)
	require.NoError(t, err)
	recorder := &assetBoundaryRecorder{}
	library := assetBoundaryLibrary(recorder)
	libraries := map[string]*Library{"native": library}
	store := newStateStore(t)
	exec := &Executor{
		DAG:            BuildSyntaxDAG(body, libraries),
		Libraries:      libraries,
		AssetCatalog:   catalog,
		AssetCache:     firstCache,
		RootAssetSetID: set.ID,
		SyntaxSource:   &body,
		Store:          store,
		Factory: state.FactoryInfo{
			Name:            "asset-boundaries",
			Version:         "v0",
			ContentRevision: "one",
		},
	}

	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	assertPlanHasOnlyLogicalAssetInputs(t, plan)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), firstRoot)
	require.NotContains(t, string(encoded), "zip bytes")
	planFile, err := DecodePlan(encoded)
	require.NoError(t, err)

	exec.AssetCache = secondCache
	result, err := exec.ApplyPlan(context.Background(), planFile)
	require.NoError(t, err)
	require.Equal(t, "zip bytes", result.Outputs["inspected"])

	for _, method := range []string{
		"resource-validate",
		"resource-create",
		"action-run",
	} {
		paths := recorder.paths(method)
		require.NotEmpty(t, paths, method)
		for _, path := range paths {
			require.True(t, strings.HasPrefix(path, secondRoot), "%s: %s", method, path)
		}
	}
	var dataPlan, dataApply bool
	for _, path := range recorder.paths("data-read") {
		dataPlan = dataPlan || strings.HasPrefix(path, firstRoot)
		dataApply = dataApply || strings.HasPrefix(path, secondRoot)
	}
	require.True(t, dataPlan)
	require.True(t, dataApply)

	snapshot, err := store.Current()
	require.NoError(t, err)
	for _, entry := range snapshot.Entries {
		assertLogicalAssetValue(t, entry.Inputs)
	}
}

func TestExecutorResolvesAssetReferencesForPriorAndLifecycleCalls(t *testing.T) {
	recorder := &assetBoundaryRecorder{}
	libraries := map[string]*Library{"native": assetBoundaryLibrary(recorder)}
	store := newStateStore(t)
	exec := &Executor{
		Libraries: libraries,
		Store:     store,
		Factory: state.FactoryInfo{
			Name:    "asset-boundaries",
			Version: "v0",
		},
	}

	createPlanRoot := filepath.Join(t.TempDir(), "create-plan")
	configureAssetBoundaryExecutor(
		t,
		exec,
		"executor",
		"one",
		createPlanRoot,
	)
	createPlan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	assertEncodedPlanOmitsAssetPayload(t, createPlan, createPlanRoot)
	applyAssetBoundaryPlan(
		t,
		exec,
		createPlan,
		filepath.Join(t.TempDir(), "create-apply"),
	)

	updatePlanRoot := filepath.Join(t.TempDir(), "update-plan")
	configureAssetBoundaryExecutor(t, exec, "executor-updated", "two", updatePlanRoot)
	updatePlan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	assertEncodedPlanOmitsAssetPayload(t, updatePlan, updatePlanRoot)
	require.Equal(t, DecisionUpdate, decisionFor(updatePlan, "resource.item"))
	assertAssetBoundaryRecordUnder(t, recorder, "resource-equivalent", updatePlanRoot)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-modify-plan", updatePlanRoot)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-modify-plan-prior", updatePlanRoot)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-read", updatePlanRoot)

	updateApplyRoot := filepath.Join(t.TempDir(), "update-apply")
	applyAssetBoundaryPlan(t, exec, updatePlan, updateApplyRoot)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-validate", updateApplyRoot)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-update", updateApplyRoot)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-update-prior", updateApplyRoot)

	replacePlanRoot := filepath.Join(t.TempDir(), "replace-plan")
	configureAssetBoundaryExecutor(t, exec, "executor-replaced", "three", replacePlanRoot)
	replacePlan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	assertEncodedPlanOmitsAssetPayload(t, replacePlan, replacePlanRoot)
	require.Equal(t, DecisionReplace, decisionFor(replacePlan, "resource.item"))
	assertAssetBoundaryRecordUnder(t, recorder, "resource-equivalent", replacePlanRoot)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-modify-plan", replacePlanRoot)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-read", replacePlanRoot)

	replaceApplyRoot := filepath.Join(t.TempDir(), "replace-apply")
	applyAssetBoundaryPlan(t, exec, replacePlan, replaceApplyRoot)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-validate", replaceApplyRoot)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-delete", replaceApplyRoot)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-create", replaceApplyRoot)

	refreshRoot := filepath.Join(t.TempDir(), "refresh")
	setAssetBoundaryCache(t, exec, refreshRoot)
	_, err = exec.Refresh(context.Background())
	require.NoError(t, err)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-read", refreshRoot)
	snapshot, err := store.Current()
	require.NoError(t, err)
	for _, entry := range snapshot.Entries {
		assertLogicalAssetValue(t, entry.Inputs)
	}

	destroyPlanRoot := filepath.Join(t.TempDir(), "destroy-plan")
	setAssetBoundaryCache(t, exec, destroyPlanRoot)
	exec.Destroy = true
	destroyPlan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	assertEncodedPlanOmitsAssetPayload(t, destroyPlan, destroyPlanRoot)
	require.Equal(t, DecisionDestroy, decisionFor(destroyPlan, "resource.item"))
	assertAssetBoundaryRecordUnder(t, recorder, "resource-read", destroyPlanRoot)

	destroyApplyRoot := filepath.Join(t.TempDir(), "destroy-apply")
	applyAssetBoundaryPlan(t, exec, destroyPlan, destroyApplyRoot)
	assertAssetBoundaryRecordUnder(t, recorder, "resource-delete", destroyApplyRoot)
	require.Empty(t, recorder.paths("resource-equivalent-invalid"))

	snapshot, err = store.Current()
	require.NoError(t, err)
	for _, entry := range snapshot.Entries {
		assertLogicalAssetValue(t, entry.Inputs)
	}
}

func TestExecutorRejectsAssetReferencesWithoutCache(t *testing.T) {
	src := ubtest.ReadInvalidFixture(
		t,
		assetBoundaryFixtureDir,
		"executor-missing-cache",
	)
	body := parseSyntaxFactoryFixture(t, src).body
	catalog, set := assetEvalCatalog(t, body, assetEvalFS("shared\n"))
	libraries := map[string]*Library{
		"native": assetBoundaryLibrary(&assetBoundaryRecorder{}),
	}
	exec := &Executor{
		DAG:            BuildSyntaxDAG(body, libraries),
		Libraries:      libraries,
		AssetCatalog:   catalog,
		RootAssetSetID: set.ID,
		SyntaxSource:   &body,
		Store:          newStateStore(t),
		Factory: state.FactoryInfo{
			Name:            "asset-boundaries",
			Version:         "v0",
			ContentRevision: "missing-cache",
		},
	}

	_, err := exec.Plan(context.Background())
	require.ErrorContains(t, err, "asset <asset.")
	require.ErrorContains(t, err, "cache is not configured")
}

func configureAssetBoundaryExecutor(
	t testing.TB,
	exec *Executor,
	fixture, revision, cacheRoot string,
) {
	t.Helper()
	src := ubtest.ReadValidFixture(t, assetBoundaryFixtureDir, fixture)
	body := parseSyntaxFactoryFixture(t, src).body
	catalog, set := assetEvalCatalog(t, body, assetEvalFS("shared\n"))
	cache, err := asset.NewCache(catalog, cacheRoot)
	require.NoError(t, err)
	exec.DAG = BuildSyntaxDAG(body, exec.Libraries)
	exec.AssetCatalog = catalog
	exec.AssetCache = cache
	exec.RootAssetSetID = set.ID
	exec.SyntaxSource = &body
	exec.Factory.ContentRevision = revision
}

func setAssetBoundaryCache(t testing.TB, exec *Executor, root string) {
	t.Helper()
	cache, err := asset.NewCache(exec.AssetCatalog, root)
	require.NoError(t, err)
	exec.AssetCache = cache
}

func applyAssetBoundaryPlan(
	t testing.TB,
	exec *Executor,
	plan *Plan,
	cacheRoot string,
) {
	t.Helper()
	assertPlanHasOnlyLogicalAssetInputs(t, plan)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), cacheRoot)
	require.NotContains(t, string(encoded), "zip bytes")
	planFile, err := DecodePlan(encoded)
	require.NoError(t, err)
	setAssetBoundaryCache(t, exec, cacheRoot)
	_, err = exec.ApplyPlan(context.Background(), planFile)
	require.NoError(t, err)
}

func assertAssetBoundaryRecordUnder(
	t testing.TB,
	recorder *assetBoundaryRecorder,
	method, root string,
) {
	t.Helper()
	for _, path := range recorder.paths(method) {
		if strings.HasPrefix(path, root) {
			return
		}
	}
	t.Errorf("%s has no path under %s: %v", method, root, recorder.paths(method))
}

func assetBoundaryLibrary(recorder *assetBoundaryRecorder) *Library {
	return &Library{
		Name: "native",
		Configuration: &cfg.ConfigurationType[*assetBoundaryConfig]{
			New: func() *assetBoundaryConfig {
				return &assetBoundaryConfig{}
			},
		},
		Resources: map[string]ResourceRegistration{
			"asset": MakeResourceWith[
				assetBoundaryResource,
				*assetBoundaryOutput,
				*assetBoundaryConfig,
			](func() *assetBoundaryResource {
				return &assetBoundaryResource{recorder: recorder}
			}),
		},
		DataSources: map[string]DataSourceRegistration{
			"asset": MakeDataSourceWith[
				assetBoundaryData,
				*assetBoundaryOutput,
				*assetBoundaryConfig,
			](func() *assetBoundaryData {
				return &assetBoundaryData{recorder: recorder}
			}),
		},
		Actions: map[string]ActionRegistration{
			"asset": MakeActionWith[
				assetBoundaryAction,
				*assetBoundaryOutput,
				*assetBoundaryConfig,
			](func() *assetBoundaryAction {
				return &assetBoundaryAction{recorder: recorder}
			}),
		},
		Functions: map[string]FunctionType{
			"inspect": MakeFunc(
				"inspect",
				"Return the content as text.",
				func(content []byte) (string, error) {
					if string(content) != "zip bytes" {
						return "", fmt.Errorf("function content was not resolved")
					}
					return string(content), nil
				},
			),
		},
	}
}

func assertPlanHasOnlyLogicalAssetInputs(t testing.TB, plan *Plan) {
	t.Helper()
	for _, step := range plan.Steps {
		assertLogicalAssetValue(t, step.Inputs)
		assertLogicalAssetValue(t, step.PriorInputs)
		assertLogicalAssetValue(t, step.PriorOutputs)
		assertLogicalAssetValue(t, step.ObservedOutputs)
	}
}

func assertEncodedPlanOmitsAssetPayload(t testing.TB, plan *Plan, cacheRoot string) {
	t.Helper()
	assertPlanHasOnlyLogicalAssetInputs(t, plan)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), cacheRoot)
	require.NotContains(t, string(encoded), "zip bytes")
}

func assertLogicalAssetValue(t testing.TB, value any) {
	t.Helper()
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, "unobin-asset:") {
			_, ok := asset.ParseReference(typed)
			require.True(t, ok)
		}
	case asset.PathRef:
		_, ok := asset.ParseReference(string(typed))
		require.True(t, ok)
	case asset.ContentRef:
		_, ok := asset.ParseReference(string(typed))
		require.True(t, ok)
	case []byte:
		t.Fatalf("logical value contains bytes")
	case []any:
		for _, element := range typed {
			assertLogicalAssetValue(t, element)
		}
	case map[string]any:
		for _, element := range typed {
			assertLogicalAssetValue(t, element)
		}
	}
}

func assetResolverCatalog(t testing.TB) (*asset.Catalog, *asset.Set) {
	t.Helper()
	body := assetBoundaryFactoryFixture(t, true)
	return assetEvalCatalog(t, body, assetEvalFS("shared\n"))
}

func assetBoundaryFactoryFixture(t testing.TB, valid bool) syntax.FactoryBody {
	t.Helper()
	var src string
	if valid {
		src = ubtest.ReadValidFixture(t, assetBoundaryFixtureDir, "function")
	} else {
		src = ubtest.ReadInvalidFixture(t, assetBoundaryFixtureDir, "function-missing-cache")
	}
	return parseSyntaxFactoryFixture(t, src).body
}
