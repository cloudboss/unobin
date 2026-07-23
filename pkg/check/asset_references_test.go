package check

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func TestAssetReferenceFixtures(t *testing.T) {
	const fixtureDir = "testdata/ub/assets"
	ubtest.RequireInvalidFixtureGoldens(t, fixtureDir)
	ubtest.Run(t, fixtureDir, func(name string, src []byte) (string, []string) {
		checker := assetFixtureChecker(t, fixtureDir, name, src)
		var unresolved []string
		errs := checker.References(func(expr lang.Expr, typ typecheck.Type) {
			path, ok := expr.(*lang.DotPath)
			if ok && path.Root.Name == "asset" && typ.ContainsUnknown() {
				unresolved = append(unresolved, fmt.Sprintf(
					"asset reference at %s retained an unknown type",
					path.Span().Start,
				))
			}
		})
		if errs.Len() == 0 {
			require.Empty(t, unresolved)
		}
		return "", assetFixtureDiagnostics(errs)
	})
}

func assetFixtureChecker(
	t *testing.T,
	fixtureDir string,
	name string,
	src []byte,
) *Checker {
	t.Helper()
	switch name {
	case "valid/composite-body", "valid/composite-second-body",
		"invalid/composite-caller-body":
		body := parseAssetComposite(t, src)
		catalog, setID := assetFixtureCatalog(t, body, "composite.ub")
		return NewSyntax(body, nil, catalog, setID)
	case "valid/composite-two-call":
		root := parseAssetFactory(t, src)
		first := parseAssetComposite(t, []byte(ubtest.ReadValidFixture(
			t,
			fixtureDir,
			"composite-body",
		)))
		second := parseAssetComposite(t, []byte(ubtest.ReadValidFixture(
			t,
			fixtureDir,
			"composite-second-body",
		)))
		catalog, rootID, firstID, secondID := assetFixtureThreeCatalog(
			t,
			root,
			first,
			second,
		)
		libs := map[string]*runtime.Library{
			"outer": {
				ResourceComposites: map[string]*runtime.CompositeType{
					"bundle": {
						Name:       "bundle",
						Kind:       runtime.NodeResource,
						SyntaxBody: &first,
						AssetSetID: firstID,
					},
					"bundle-two": {
						Name:       "bundle-two",
						Kind:       runtime.NodeResource,
						SyntaxBody: &second,
						AssetSetID: secondID,
					},
				},
			},
		}
		return NewSyntax(root, libs, catalog, rootID)
	case "valid/composite-call", "invalid/composite-caller-call":
		root := parseAssetFactory(t, src)
		bodyName := "composite-body"
		if strings.HasPrefix(name, "invalid/") {
			bodyName = "composite-caller-body"
		}
		bodySrc := ubtest.ReadFixture(
			t,
			filepath.Join(
				fixtureDir,
				strings.Split(name, "/")[0],
				bodyName+".ub",
			),
		)
		composite := parseAssetComposite(t, []byte(bodySrc))
		catalog, rootID, compositeID := assetFixtureCombinedCatalog(t, root, composite)
		libs := map[string]*runtime.Library{
			"outer": {
				ResourceComposites: map[string]*runtime.CompositeType{
					"bundle": {
						Name:       "bundle",
						Kind:       runtime.NodeResource,
						SyntaxBody: &composite,
						AssetSetID: compositeID,
					},
				},
			},
		}
		return NewSyntax(root, libs, catalog, rootID)
	default:
		body := parseAssetFactory(t, src)
		catalog, setID := assetFixtureCatalog(t, body, "factory.ub")
		return NewSyntax(body, assetReferenceLibraries(), catalog, setID)
	}
}

func parseAssetFactory(t *testing.T, src []byte) syntax.FactoryBody {
	t.Helper()
	file, err := syntax.ParseSource("factory.ub", src)
	require.NoError(t, err)
	require.NotNil(t, file.Factory)
	return file.Factory.Body
}

func parseAssetComposite(t *testing.T, src []byte) syntax.FactoryBody {
	t.Helper()
	file, err := syntax.ParseSource("composite.ub", src)
	require.NoError(t, err)
	require.NotNil(t, file.Library)
	require.Len(t, file.Library.Exports, 1)
	return file.Library.Exports[0].Body
}

func assetFixtureCatalog(
	t *testing.T,
	body syntax.FactoryBody,
	sourceFile string,
) (*asset.Catalog, string) {
	t.Helper()
	collection := &asset.Collection{}
	set := captureAssetFixtureSet(t, body, sourceFile)
	require.NoError(t, collection.Add(set))
	return collection.Catalog(), set.ID
}

func assetFixtureCombinedCatalog(
	t *testing.T,
	root syntax.FactoryBody,
	composite syntax.FactoryBody,
) (*asset.Catalog, string, string) {
	t.Helper()
	collection := &asset.Collection{}
	rootSet := captureAssetFixtureSet(t, root, "factory.ub")
	compositeSet := captureAssetFixtureSet(t, composite, "composite.ub")
	require.NoError(t, collection.Add(rootSet))
	require.NoError(t, collection.Add(compositeSet))
	return collection.Catalog(), rootSet.ID, compositeSet.ID
}

func assetFixtureThreeCatalog(
	t *testing.T,
	root syntax.FactoryBody,
	first syntax.FactoryBody,
	second syntax.FactoryBody,
) (*asset.Catalog, string, string, string) {
	t.Helper()
	collection := &asset.Collection{}
	rootSet := captureAssetFixtureSet(t, root, "factory.ub")
	firstSet := captureAssetFixtureSet(t, first, "composite.ub")
	secondSet := captureAssetFixtureSet(t, second, "composite.ub")
	require.NoError(t, collection.Add(rootSet))
	require.NoError(t, collection.Add(firstSet))
	require.NoError(t, collection.Add(secondSet))
	return collection.Catalog(), rootSet.ID, firstSet.ID, secondSet.ID
}

func captureAssetFixtureSet(
	t *testing.T,
	body syntax.FactoryBody,
	sourceFile string,
) *asset.CapturedSet {
	t.Helper()
	projectFS := assetReferenceFS()
	set, err := asset.Capture(
		&resolve.Source{FS: projectFS, ProjectFS: projectFS},
		syntax.SourceFileSpec{ProjectRelPath: sourceFile},
		body.Assets,
		"",
	)
	require.NoError(t, err)
	require.NotNil(t, set)
	return set
}

func assetReferenceFS() fs.FS {
	return fstest.MapFS{
		"archive.zip": {
			Data: []byte("zip bytes"),
			Mode: 0o644,
		},
		"tree": {
			Mode: fs.ModeDir | 0o755,
		},
		"tree/main.go": {
			Data: []byte("package main\n"),
			Mode: 0o644,
		},
		"tree/internal": {
			Mode: fs.ModeDir | 0o755,
		},
		"tree/internal/helpers.go": {
			Data: []byte("package internal\n"),
			Mode: 0o644,
		},
		"root-tree": {
			Mode: fs.ModeDir | 0o755,
		},
		"root-tree/root.txt": {
			Data: []byte("root"),
			Mode: 0o644,
		},
		"composite-tree": {
			Mode: fs.ModeDir | 0o755,
		},
		"composite-tree/composite.txt": {
			Data: []byte("composite"),
			Mode: 0o644,
		},
		"second-tree": {
			Mode: fs.ModeDir | 0o755,
		},
		"second-tree/second.txt": {
			Data: []byte("second"),
			Mode: 0o644,
		},
	}
}

func assetReferenceLibraries() map[string]*runtime.Library {
	nested := typecheck.TObject([]typecheck.ObjectField{
		{Name: "paths", Type: typecheck.TList(typecheck.TString())},
		{Name: "content", Type: typecheck.TBytes()},
	})
	return map[string]*runtime.Library{
		"native": {
			Schema: &runtime.LibrarySchema{
				Resources: map[string]*runtime.TypeSchema{
					"sink": {
						Inputs: map[string]typecheck.Type{
							"path":    typecheck.TString(),
							"content": typecheck.TBytes(),
							"nested":  nested,
						},
					},
				},
				Functions: map[string]typecheck.FuncSig{
					"accept-path": {
						Params: []typecheck.Type{typecheck.TString()},
						Result: typecheck.TString(),
					},
					"accept-content": {
						Params: []typecheck.Type{typecheck.TBytes()},
						Result: typecheck.TString(),
					},
				},
			},
		},
	}
}

func assetFixtureDiagnostics(errs *lang.ErrorList) []string {
	out := make([]string, 0, errs.Len())
	for _, err := range errs.Errors() {
		message := err.Msg
		if err.Hint != "" {
			message += "\n  hint: " + err.Hint
		}
		out = append(out, message)
	}
	return out
}
