package codegen

import (
	"go/parser"
	"go/token"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestGenerateLibraryUsesCompositeInputs(t *testing.T) {
	source := ubtest.ReadValidFixture(t, "testdata/ub/emission", "library")
	file, err := syntax.ParseSource("library.ub", []byte(source))
	require.NoError(t, err)
	body := file.Library.Exports[0].Body
	config := runtime.LibraryConfigSchema{Path: "example.com/core", Empty: true}
	input := program.Library{
		Name: "metadata",
		Composites: []program.Composite{{
			Category: "resource", Export: "wrapper", Body: body, AssetSetID: "captured-assets",
			Imports: []resolve.Resolution{
				{Kind: resolve.ResolutionGo, LocalAlias: "core", Path: "example.com/core"},
				{Kind: resolve.ResolutionUB, LocalAlias: "child", CanonicalKey: "local:child"},
			},
			LibraryConfigSchemas: map[string]runtime.LibraryConfigSchema{
				"example.com/core": config,
			},
		}},
		SourceFiles: map[string]syntax.SourceFileSpec{
			"library.ub": {DisplayPath: "library.ub", LineStarts: parse.LineStarts([]byte(source))},
		},
	}
	actual, err := GenerateLibrary("metadata", input,
		map[string]string{"local:child": "benchmark-stack/internal/child"})
	require.NoError(t, err)
	expected, err := GenerateUBLibraryPackageWithAssetsAndConfigSchemas(
		"metadata", "metadata",
		resourceSyntaxBodies(map[string]syntax.FactoryBody{"wrapper": body}),
		compositeImports("resource", map[string]map[string]string{"wrapper": {
			"core": "example.com/core", "child": "benchmark-stack/internal/child",
		}}), nil, input.SourceFiles,
		map[string]map[string]string{"resource": {"wrapper": "captured-assets"}},
		map[string]map[string]map[string]runtime.LibraryConfigSchema{
			"resource": {"wrapper": {"example.com/core": config}},
		},
	)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	_, err = parser.ParseFile(token.NewFileSet(), "metadata.go", actual, parser.AllErrors)
	require.NoError(t, err)
	_, err = GenerateLibrary("metadata", input, nil)
	require.ErrorContains(t, err, "missing package path")
	require.Equal(t, []string{"core", "child"}, []string{
		input.Composites[0].Imports[0].LocalAlias, input.Composites[0].Imports[1].LocalAlias,
	})
}

func TestGenerateLibraryPreservesNamespacesAndOrder(t *testing.T) {
	library := program.Library{Name: "metadata", Composites: []program.Composite{
		{Category: "resource", Export: "shared"},
		{Category: "action", Export: "shared"},
		{Category: "data-source", Export: "shared"},
	}}
	first, err := GenerateLibrary("metadata", library, nil)
	require.NoError(t, err)
	slices.Reverse(library.Composites)
	second, err := GenerateLibrary("metadata", library, nil)
	require.NoError(t, err)
	require.Equal(t, first, second)
	for _, category := range []string{"ResourceComposites", "DataComposites", "ActionComposites"} {
		require.Contains(t, string(first), category+": map[string]*runtime.CompositeType{")
	}
	require.Equal(t, []string{"data-source", "action", "resource"}, []string{
		library.Composites[0].Category, library.Composites[1].Category,
		library.Composites[2].Category,
	})
}

func TestGenerateLibraryRejectsDuplicateImportAliases(t *testing.T) {
	_, err := GenerateLibrary("metadata", program.Library{
		Name: "metadata", Composites: []program.Composite{{
			Category: "resource", Export: "wrapper", Imports: []resolve.Resolution{
				{Kind: resolve.ResolutionGo, LocalAlias: "core", Path: "example.com/one"},
				{Kind: resolve.ResolutionGo, LocalAlias: "core", Path: "example.com/two"},
			},
		}},
	}, nil)
	require.ErrorContains(t, err, "resource export \"wrapper\": duplicate import alias \"core\"")
}

func TestGenerateLibraryRejectsDuplicateExports(t *testing.T) {
	_, err := GenerateLibrary("metadata", program.Library{
		Name: "metadata", Composites: []program.Composite{
			{Category: "resource", Export: "wrapper"},
			{Category: "resource", Export: "wrapper"},
		},
	}, nil)
	require.ErrorContains(t, err, "duplicate resource export")
}
