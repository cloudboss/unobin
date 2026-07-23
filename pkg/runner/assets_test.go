package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestAssetCacheFlagIsPersistentAndLazy(t *testing.T) {
	info := runnerAssetInfo(t)
	cacheRoot := filepath.Join(t.TempDir(), "cache")

	for _, args := range [][]string{
		{"--asset-cache-dir", cacheRoot, "version"},
		{"version", "--asset-cache-dir", cacheRoot},
	} {
		root := newRootCmd(info)
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs(args)

		require.NoError(t, root.Execute())
		require.Contains(t, output.String(), "asset-factory")
		require.NoDirExists(t, cacheRoot)
	}

	root := newRootCmd(info)
	require.NotNil(t, root.PersistentFlags().Lookup("asset-cache-dir"))
	version, _, err := root.Find([]string{"version"})
	require.NoError(t, err)
	require.NotNil(t, version.InheritedFlags().Lookup("asset-cache-dir"))

	for _, args := range [][]string{{"--help"}, {"plan", "--help"}} {
		root := newRootCmd(info)
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetArgs(args)
		require.NoError(t, root.Execute())
		require.Contains(t, output.String(), "--asset-cache-dir")
	}
}

func TestAssetMetadataCommandsDoNotCreateCache(t *testing.T) {
	tests := []struct {
		name string
		args func(testing.TB) []string
	}{
		{
			name: "version",
			args: func(testing.TB) []string {
				return []string{"version"}
			},
		},
		{
			name: "schema show",
			args: func(testing.TB) []string {
				return []string{"schema", "show"}
			},
		},
		{
			name: "pin",
			args: func(t testing.TB) []string {
				configPath := filepath.Join(t.TempDir(), "stack.ub")
				require.NoError(
					t,
					os.WriteFile(
						configPath,
						[]byte(pinFixture(t, "stack-factory-no-pin-src")),
						0o600,
					),
				)
				return []string{"pin", "--config", configPath}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := runnerAssetInfo(t)
			cacheRoot := filepath.Join(t.TempDir(), "cache")
			root := newRootCmd(info)
			root.SetOut(&bytes.Buffer{})
			root.SetArgs(append(tt.args(t), "--asset-cache-dir", cacheRoot))

			require.NoError(t, root.Execute())
			require.NoDirExists(t, cacheRoot)
		})
	}
}

func TestLoadRunnerAssetsNormalizesRelativeCacheRoot(t *testing.T) {
	info := runnerAssetInfo(t)
	relative := filepath.Join("relative", "cache")

	loaded, err := loadRunnerAssets(info, relative)
	require.NoError(t, err)
	root, err := loaded.cache.Root()
	require.NoError(t, err)
	expected, err := filepath.Abs(relative)
	require.NoError(t, err)
	require.Equal(t, expected, root)
	require.NoDirExists(t, root)
}

func TestRunnerDefersInvalidCacheRootUntilResolution(t *testing.T) {
	info := runnerAssetInfo(t)
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	require.NoError(t, os.WriteFile(cacheRoot, []byte("not a directory"), 0o600))
	root := newRootCmd(info)
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"version", "--asset-cache-dir", cacheRoot})

	require.NoError(t, root.Execute())

	loaded, err := loadRunnerAssets(info, cacheRoot)
	require.NoError(t, err)
	set, ok := loaded.catalog.Set(info.RootAssetSetID)
	require.True(t, ok)
	value, err := set.Value("payload", "")
	require.NoError(t, err)
	_, err = loaded.cache.Resolve(string(value.Path))
	require.Error(t, err)
}

func TestRunnerHidesCacheRootInAssetErrors(t *testing.T) {
	info := runnerAssetInfo(t)
	cacheRoot := filepath.Join(t.TempDir(), "private-cache")
	loaded, err := loadRunnerAssets(info, cacheRoot)
	require.NoError(t, err)
	set, ok := loaded.catalog.Set(info.RootAssetSetID)
	require.True(t, ok)
	value, err := set.Value("payload", "")
	require.NoError(t, err)
	reference, ok := asset.ParseReference(string(value.Path))
	require.True(t, ok)
	missing := strings.Replace(
		string(value.Path),
		reference.EntryIdentity,
		strings.Repeat("0", 64),
		1,
	)

	_, err = loaded.cache.Resolve(missing)
	require.Error(t, err)
	require.NotContains(t, err.Error(), cacheRoot)
	require.Contains(t, err.Error(), "cache private-cache")
}

func TestRunnerRejectsInvalidAssetBundleAfterFlagParsing(t *testing.T) {
	info := runnerAssetInfo(t)
	info.AssetBundle = []byte("not a bundle")
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	root := newRootCmd(info)
	root.SetArgs([]string{"version", "--asset-cache-dir", cacheRoot})

	err := root.Execute()
	require.ErrorContains(t, err, "asset bundle")
	require.NoDirExists(t, cacheRoot)
}

func TestRunnerRejectsMissingRootAssetSet(t *testing.T) {
	info := runnerAssetInfo(t)
	info.RootAssetSetID = strings.Repeat("0", 64)
	root := newRootCmd(info)
	root.SetArgs([]string{"version"})

	err := root.Execute()
	require.EqualError(
		t,
		err,
		`asset set "`+info.RootAssetSetID+`": not found in asset bundle`,
	)
}

func TestRunnerRejectsRootAssetSetWithoutBundle(t *testing.T) {
	body := runnerEmptyFactoryBody(t)
	info := Info{
		FactoryName:    "asset-factory",
		FactoryBody:    &body,
		RootAssetSetID: strings.Repeat("0", 64),
	}
	root := newRootCmd(info)
	root.SetArgs([]string{"version"})

	err := root.Execute()
	require.EqualError(
		t,
		err,
		`asset set "`+info.RootAssetSetID+`": not found in asset bundle`,
	)
}

func TestRunnerRejectsMissingCompositeAssetSet(t *testing.T) {
	info := runnerAssetInfo(t)
	src := ubtest.ReadInvalidFixture(
		t,
		"testdata/ub/assets-runner",
		"composite-missing-set",
	)
	body := testFactoryBody(t, src)
	compositeSource := ubtest.ReadValidFixture(
		t,
		"testdata/ub/assets-runner",
		"empty-composite",
	)
	compositeBody := runnerCompositeBody(t, compositeSource)
	missingID := strings.Repeat("1", 64)
	info.FactoryBody = &body
	info.Libraries = map[string]*runtime.Library{
		"components": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"group": {
					Name:       "group",
					SyntaxBody: &compositeBody,
					AssetSetID: missingID,
				},
			},
		},
	}
	root := newRootCmd(info)
	root.SetArgs([]string{"version"})

	err := root.Execute()
	require.EqualError(
		t,
		err,
		`asset set "`+missingID+
			`" for composite resource.item: not found in asset bundle`,
	)
}

func TestRunnerAllowsCompositeAssetsWithoutRootAssets(t *testing.T) {
	info, childSetID := runnerAssetSetsInfo(t)
	src := ubtest.ReadValidFixture(t, "testdata/ub/assets-runner", "composite-call")
	body := testFactoryBody(t, src)
	compositeSource := ubtest.ReadValidFixture(
		t,
		"testdata/ub/assets-runner",
		"empty-composite",
	)
	compositeBody := runnerCompositeBody(t, compositeSource)
	info.FactoryBody = &body
	info.RootAssetSetID = ""
	info.Libraries = map[string]*runtime.Library{
		"components": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"group": {
					Name:       "group",
					SyntaxBody: &compositeBody,
					AssetSetID: childSetID,
				},
			},
		},
	}
	root := newRootCmd(info)
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"version"})

	require.NoError(t, root.Execute())
}

func TestRunnerAssetsConfigureExecutor(t *testing.T) {
	info := runnerAssetInfo(t)
	loaded, err := loadRunnerAssets(info, filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)
	exec := &runtime.Executor{}

	loaded.configureExecutor(exec)

	require.Same(t, loaded.catalog, exec.AssetCatalog)
	require.Same(t, loaded.cache, exec.AssetCache)
	require.Equal(t, info.RootAssetSetID, exec.RootAssetSetID)
}

func TestRunnerAssetStartupUsesMachineErrorContract(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "format flag", args: []string{"version", "--format", "json"}},
		{
			name: "deprecated apply output flag",
			args: []string{"apply", "missing.plan", "--output", "json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := runnerAssetInfo(t)
			info.AssetBundle = []byte("not a bundle")
			root := newRootCmd(info)
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetArgs(tt.args)

			err := root.Execute()
			require.Error(t, err)
			require.Contains(t, output.String(), `"kind":"command-error"`)
			require.Contains(t, output.String(), "asset bundle")
		})
	}
}

func TestValidateUsesLexicalAssetSets(t *testing.T) {
	valid := runnerLexicalAssetInfo(
		t,
		"valid/lexical-root.ub",
		"valid/lexical-composite.ub",
	)
	require.NoError(t, validateStack(valid, nil, ""))

	tests := []struct {
		name             string
		rootFixture      string
		compositeFixture string
		want             string
	}{
		{
			name:             "root uses composite asset",
			rootFixture:      "invalid/root-uses-child.ub",
			compositeFixture: "valid/lexical-composite.ub",
			want:             `resolve: unknown asset "child"`,
		},
		{
			name:             "composite uses root asset",
			rootFixture:      "valid/lexical-root.ub",
			compositeFixture: "invalid/composite-uses-root.ub",
			want:             `resolve: unknown asset "payload"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := runnerLexicalAssetInfo(
				t,
				tt.rootFixture,
				tt.compositeFixture,
			)

			err := validateStack(info, nil, "")
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestRunnerWithoutAssetsKeepsVersionBehavior(t *testing.T) {
	body := runnerEmptyFactoryBody(t)
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	root := newRootCmd(Info{
		FactoryName:     "plain-factory",
		FactoryVersion:  "v1",
		ContentRevision: "one",
		FactoryBody:     &body,
	})
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"version", "--asset-cache-dir", cacheRoot})

	require.NoError(t, root.Execute())
	require.Contains(t, output.String(), "plain-factory v1")
	require.NoDirExists(t, cacheRoot)
}

func runnerAssetInfo(t testing.TB) Info {
	t.Helper()
	info, _ := runnerAssetSetsInfo(t)
	return info
}

func runnerAssetSetsInfo(t testing.TB) (Info, string) {
	t.Helper()
	files := fstest.MapFS{
		"factory.ub": &fstest.MapFile{},
		"payload":    &fstest.MapFile{Data: []byte("payload\n")},
		"child":      &fstest.MapFile{Data: []byte("child\n")},
	}
	set, err := asset.Capture(
		&resolve.Source{FS: files, ProjectFS: files},
		syntax.SourceFileSpec{ProjectRelPath: "factory.ub"},
		[]syntax.AssetDecl{{
			Name:   syntax.Ident{Name: "payload"},
			Source: &parse.StringLit{Value: "payload"},
		}},
		"",
	)
	require.NoError(t, err)
	childSet, err := asset.Capture(
		&resolve.Source{FS: files, ProjectFS: files},
		syntax.SourceFileSpec{ProjectRelPath: "factory.ub"},
		[]syntax.AssetDecl{{
			Name:   syntax.Ident{Name: "child"},
			Source: &parse.StringLit{Value: "child"},
		}},
		"",
	)
	require.NoError(t, err)
	var collection asset.Collection
	require.NoError(t, collection.Add(set))
	require.NoError(t, collection.Add(childSet))
	bundle, err := collection.Encode()
	require.NoError(t, err)
	body := runnerEmptyFactoryBody(t)
	return Info{
		FactoryName:     "asset-factory",
		FactoryVersion:  "v1",
		ContentRevision: "one",
		FactoryBody:     &body,
		AssetBundle:     bundle,
		RootAssetSetID:  set.ID,
	}, childSet.ID
}

func runnerLexicalAssetInfo(
	t testing.TB,
	rootFixture, compositeFixture string,
) Info {
	t.Helper()
	info, childSetID := runnerAssetSetsInfo(t)
	rootSource := ubtest.ReadFixture(
		t,
		filepath.Join("testdata/ub/assets-runner", rootFixture),
	)
	rootBody := testFactoryBody(t, rootSource)
	compositeSource := ubtest.ReadFixture(
		t,
		filepath.Join("testdata/ub/assets-runner", compositeFixture),
	)
	compositeBody := runnerCompositeBody(t, compositeSource)
	info.FactoryBody = &rootBody
	info.Libraries = map[string]*runtime.Library{
		"components": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"group": {
					Name:       "group",
					SyntaxBody: &compositeBody,
					AssetSetID: childSetID,
				},
			},
		},
	}
	return info
}

func runnerCompositeBody(t testing.TB, source string) syntax.FactoryBody {
	t.Helper()
	parsed, err := syntax.ParseSource("composite.ub", []byte(source))
	require.NoError(t, err)
	require.Equal(t, syntax.FileLibrary, parsed.Kind)
	require.NotNil(t, parsed.Library)
	require.Len(t, parsed.Library.Exports, 1)
	return parsed.Library.Exports[0].Body
}

func runnerEmptyFactoryBody(t testing.TB) syntax.FactoryBody {
	t.Helper()
	source := ubtest.ReadValidFixture(
		t,
		"testdata/ub/assets-runner",
		"empty-factory",
	)
	return testFactoryBody(t, source)
}
