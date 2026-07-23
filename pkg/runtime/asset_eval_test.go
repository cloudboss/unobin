package runtime

import (
	"encoding/base64"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
)

const assetEvalFixtureDir = "testdata/ub/asset-eval"

func TestEvalAssetObjectsAndAttributes(t *testing.T) {
	body := assetEvalFactoryFixture(t, "attributes")
	catalog, set := assetEvalCatalog(t, body, assetEvalFS("shared\n"))
	ctx := &EvalContext{Assets: set, locals: newLocalScopeFromMap(syntaxLocalMap(body.Locals))}

	root, err := Eval(assetEvalLocal(t, body, "root"), ctx)
	require.NoError(t, err)
	rootValue := requireAssetValue(t, root)
	rootEntry := assetEvalEntry(t, set, "tree", "")
	require.Equal(t, rootEntry.ContentSHA256, rootValue.ContentSHA256)
	require.Equal(t, rootEntry.Mode, rootValue.Mode)

	selected, err := Eval(assetEvalLocal(t, body, "selected"), ctx)
	require.NoError(t, err)
	selectedValue := requireAssetValue(t, selected)
	selectedEntry := assetEvalEntry(t, set, "tree", "main.go")
	require.Equal(t, selectedEntry.ContentSHA256, selectedValue.ContentSHA256)
	require.Equal(t, selectedEntry.Mode, selectedValue.Mode)

	values, err := Eval(assetEvalLocal(t, body, "values"), ctx)
	require.NoError(t, err)
	got := values.(map[string]any)
	require.Equal(t, rootValue.Path, got["tree-path"])
	require.Equal(t, rootValue.Content, got["tree-content"])
	require.Equal(t, rootEntry.ContentSHA256, got["tree-digest"])
	require.Equal(t, rootEntry.Mode, got["tree-mode"])
	require.Equal(t, selectedValue.Path, got["file-path"])
	require.Equal(t, selectedValue.Content, got["file-content"])
	require.Equal(t, selectedEntry.ContentSHA256, got["file-digest"])
	require.Equal(t, selectedEntry.Mode, got["file-mode"])
	require.Equal(t, true, got["same-path"])
	require.Equal(t, true, got["other-path"])
	require.Equal(t, true, got["same-content"])
	require.Equal(t, true, got["other-content"])
	require.Equal(
		t,
		base64.StdEncoding.EncodeToString([]byte(selectedValue.Path)),
		got["encoded-path"],
	)
	require.Equal(
		t,
		base64.StdEncoding.EncodeToString([]byte("zip bytes")),
		got["encoded-content"],
	)

	pathReference, ok := catalog.Reference(string(selectedValue.Path))
	require.True(t, ok)
	require.Same(t, selectedEntry, pathReference.Entry)
}

func TestEvalAssetValueIsKnownDuringPartialEvaluation(t *testing.T) {
	body := assetEvalFactoryFixture(t, "attributes")
	_, set := assetEvalCatalog(t, body, assetEvalFS("shared\n"))
	node := ExtractSyntaxNodes(body, nil)[0]
	ctx := &EvalContext{Assets: set}

	inputs, unresolved, err := planEvalBody(node.Body, ctx)
	require.NoError(t, err)
	require.Empty(t, unresolved)
	require.IsType(t, asset.PathRef(""), inputs["source"])
	require.IsType(t, asset.ContentRef(""), inputs["payload"])
}

func TestEvalAssetErrors(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "no-set",
			want: "eval: asset.tree: no asset set is available in this scope",
		},
		{
			name: "unknown-asset",
			want: `eval: asset.missing: asset "missing" is not in asset set`,
		},
		{
			name: "missing-entry",
			want: `eval: asset.tree['missing.txt']: asset "tree" has no entry "missing.txt"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := assetEvalInvalidFactoryFixture(t, tt.name)
			var set *asset.Set
			if len(body.Assets) > 0 {
				_, set = assetEvalCatalog(t, body, assetEvalFS("shared\n"))
			}
			_, err := Eval(assetEvalLocal(t, body, "result"), &EvalContext{Assets: set})
			require.EqualError(t, err, tt.want)
		})
	}
}

func TestCompositeScopeUsesItsAssetSet(t *testing.T) {
	body := assetEvalCompositeFixture(t)
	firstCatalog, firstSet := assetEvalCatalog(t, body, assetEvalFS("first\n"))
	secondCatalog, secondSet := assetEvalCatalog(t, body, assetEvalFS("second\n"))
	combined := assetEvalCombinedCatalog(t, body)
	firstCombined, ok := combined.Set(firstSet.ID)
	require.True(t, ok)
	secondCombined, ok := combined.Set(secondSet.ID)
	require.True(t, ok)
	require.NotEqual(t, firstCatalog.Sets()[0].ID, secondCatalog.Sets()[0].ID)

	firstNode := &Node{
		Address:             "resource.first",
		Kind:                NodeResource,
		Body:                &lang.ObjectLit{},
		CompositeSyntaxBody: &body,
		AssetSetID:          firstCombined.ID,
	}
	secondNode := &Node{
		Address:             "resource.second",
		Kind:                NodeResource,
		Body:                &lang.ObjectLit{},
		CompositeSyntaxBody: &body,
		AssetSetID:          secondCombined.ID,
	}
	exec := &Executor{
		DAG: &DAG{Nodes: map[string]*Node{
			firstNode.Address:  firstNode,
			secondNode.Address: secondNode,
		}},
		AssetCatalog: combined,
	}
	rs := &runState{
		eval:       &EvalContext{},
		composites: map[string]*EvalContext{},
	}

	firstScope, err := exec.ensureCompositeScope(rs, firstNode.Address)
	require.NoError(t, err)
	firstAgain, err := exec.ensureCompositeScope(rs, firstNode.Address)
	require.NoError(t, err)
	require.Same(t, firstScope, firstAgain)
	require.Same(t, firstCombined, firstScope.Assets)

	secondScope, err := exec.ensureCompositeScope(rs, secondNode.Address)
	require.NoError(t, err)
	require.Same(t, secondCombined, secondScope.Assets)

	firstValue, err := Eval(assetEvalLocal(t, body, "source"), firstScope)
	require.NoError(t, err)
	secondValue, err := Eval(assetEvalLocal(t, body, "source"), secondScope)
	require.NoError(t, err)
	require.NotEqual(t, firstValue, secondValue)

	internal := ExtractSyntaxNodes(body, nil)[0]
	firstInputs, err := evalBody(internal.Body, firstScope)
	require.NoError(t, err)
	require.Equal(t, firstValue, firstInputs["source"])
}

func TestExecutorRejectsMissingAssetCatalogEntries(t *testing.T) {
	exec := &Executor{RootAssetSetID: "missing"}
	_, err := exec.rootAssetSet()
	require.EqualError(t, err, `asset set "missing": asset catalog is not configured`)

	exec.AssetCatalog = (&asset.Collection{}).Catalog()
	_, err = exec.rootAssetSet()
	require.EqualError(t, err, `asset set "missing": not found in asset catalog`)
}

func requireAssetValue(t testing.TB, value any) asset.Value {
	t.Helper()
	typed, ok := value.(asset.Value)
	require.True(t, ok)
	return typed
}

func assetEvalEntry(
	t testing.TB,
	set *asset.Set,
	name string,
	internalPath string,
) *asset.Entry {
	t.Helper()
	item, ok := set.Asset(name)
	require.True(t, ok)
	entry, ok := item.Entry(internalPath)
	require.True(t, ok)
	return entry
}

func assetEvalFactoryFixture(t testing.TB, name string) syntax.FactoryBody {
	t.Helper()
	src := ubtest.ReadValidFixture(t, assetEvalFixtureDir, name)
	return parseSyntaxFactoryFixture(t, src).body
}

func assetEvalInvalidFactoryFixture(t testing.TB, name string) syntax.FactoryBody {
	t.Helper()
	src := ubtest.ReadInvalidFixture(t, assetEvalFixtureDir, name)
	return parseSyntaxFactoryFixture(t, src).body
}

func assetEvalCompositeFixture(t testing.TB) syntax.FactoryBody {
	t.Helper()
	src := ubtest.ReadValidFixture(t, assetEvalFixtureDir, "composite")
	return parseSyntaxCompositeFixture(t, src).body
}

func assetEvalLocal(t testing.TB, body syntax.FactoryBody, name string) lang.Expr {
	t.Helper()
	for _, local := range body.Locals {
		if local.Name.Name == name {
			return local.Value
		}
	}
	t.Fatalf("local %q not found", name)
	return nil
}

func assetEvalCatalog(
	t testing.TB,
	body syntax.FactoryBody,
	projectFS fstest.MapFS,
) (*asset.Catalog, *asset.Set) {
	t.Helper()
	captured, err := asset.Capture(
		&resolve.Source{FS: projectFS, ProjectFS: projectFS},
		syntax.SourceFileSpec{ProjectRelPath: "factory.ub"},
		body.Assets,
		"",
	)
	require.NoError(t, err)
	var collection asset.Collection
	require.NoError(t, collection.Add(captured))
	catalog := collection.Catalog()
	set, ok := catalog.Set(captured.ID)
	require.True(t, ok)
	return catalog, set
}

func assetEvalCombinedCatalog(t testing.TB, body syntax.FactoryBody) *asset.Catalog {
	t.Helper()
	first, err := asset.Capture(
		&resolve.Source{
			FS:        assetEvalFS("first\n"),
			ProjectFS: assetEvalFS("first\n"),
		},
		syntax.SourceFileSpec{ProjectRelPath: "factory.ub"},
		body.Assets,
		"",
	)
	require.NoError(t, err)
	secondFS := assetEvalFS("second\n")
	second, err := asset.Capture(
		&resolve.Source{FS: secondFS, ProjectFS: secondFS},
		syntax.SourceFileSpec{ProjectRelPath: "factory.ub"},
		body.Assets,
		"",
	)
	require.NoError(t, err)
	var collection asset.Collection
	require.NoError(t, collection.Add(first))
	require.NoError(t, collection.Add(second))
	return collection.Catalog()
}

func assetEvalFS(shared string) fstest.MapFS {
	return fstest.MapFS{
		"factory.ub":   &fstest.MapFile{},
		"tree":         &fstest.MapFile{Mode: fs.ModeDir | 0o755},
		"tree/main.go": &fstest.MapFile{Data: []byte("package main\n"), Mode: 0o644},
		"archive.zip":  &fstest.MapFile{Data: []byte("zip bytes"), Mode: 0o644},
		"shared.txt":   &fstest.MapFile{Data: []byte(shared), Mode: 0o644},
	}
}
