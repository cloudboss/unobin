package codegen

import (
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestGenerateImportsUsesCanonicalLibraries(t *testing.T) {
	imports := &program.Imports{
		Top: []resolve.Resolution{
			{Kind: resolve.ResolutionGo, LocalAlias: "std", Path: "example.com/std"},
			{Kind: resolve.ResolutionUB, LocalAlias: "first", CanonicalKey: "local:/tmp/app/a"},
			{Kind: resolve.ResolutionUB, LocalAlias: "again", CanonicalKey: "local:/tmp/app/a"},
			{Kind: resolve.ResolutionUB, LocalAlias: "other", CanonicalKey: "local:/tmp/app/b"},
		},
		UBLibraries: []program.Library{
			{Name: "a", CanonicalKey: "local:/tmp/app/a"},
			{Name: "a", CanonicalKey: "local:/tmp/app/b"},
		},
	}
	generated, err := GenerateImports(imports, "factory")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"std": "example.com/std"}, generated.GoImports)
	require.Equal(t, map[string]string{
		"first": "factory/internal/a", "again": "factory/internal/a",
		"other": "factory/internal/a_c82f8103",
	}, generated.UBImports)
	require.Equal(t, []string{"a", "a_c82f8103"}, slices.Sorted(maps.Keys(generated.UBPackages)))
	for name, source := range generated.UBPackages {
		_, err := parser.ParseFile(token.NewFileSet(), name+".go", source, parser.AllErrors)
		require.NoError(t, err)
	}
}

func TestGenerateImportsRejectsInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		name    string
		imports *program.Imports
		message string
	}{
		{name: "nil", message: "import analysis is nil"},
		{name: "missing library", message: "missing library", imports: &program.Imports{
			Top: []resolve.Resolution{{
				Kind: resolve.ResolutionUB, LocalAlias: "missing", CanonicalKey: "local:/missing",
			}},
		}},
		{name: "duplicate library", message: "duplicate library", imports: &program.Imports{
			UBLibraries: []program.Library{
				{Name: "first", CanonicalKey: "local:/shared"},
				{Name: "second", CanonicalKey: "local:/shared"},
			},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := GenerateImports(test.imports, "factory")
			require.ErrorContains(t, err, test.message)
		})
	}
}
