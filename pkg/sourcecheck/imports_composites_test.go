package sourcecheck

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/cloudboss/unobin/pkg/codegen"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func TestImportVisitorBuildsCompositeArtifactsTogether(t *testing.T) {
	goSourcePath := t.TempDir()
	schemas := NewSchemaCacheWithReader(
		func(sourcePath string) (*runtime.LibrarySchema, []string, error) {
			require.Equal(t, goSourcePath, sourcePath)
			return compositeArtifactSchema(), nil, nil
		},
	)
	visitor := newImportVisitor(ImportAnalysisOptions{
		GeneratePackages: true,
		StackName:        "demo",
	}, schemas)
	nestedLib := &runtime.Library{Name: "leaf"}
	visitor.runtimeLibraries["local:/leaf"] = nestedLib
	require.Equal(t, "leaf", visitor.packageIDs.ID("leaf", "local:/leaf"))

	lib := compositeArtifactLibrary(goSourcePath)
	composites, err := visitor.buildCompiledComposites(lib.CompositeEntries(), lib.BodyImports)
	require.NoError(t, err)
	require.Len(t, composites, 2)

	byName := map[string]compiledComposite{}
	for _, composite := range composites {
		byName[composite.entry.Name] = composite
	}

	archive := byName["archive"]
	require.Equal(t, resolve.CompositeEntry{
		Kind:       "resource",
		Name:       "archive",
		SyntaxBody: lib.SyntaxBodies["resource"]["archive"],
	}, archive.entry)
	require.Same(t, nestedLib, archive.bodyLibs["leaf"])
	require.NotNil(t, archive.bodyLibs["std"].Schema)
	require.Equal(t, map[string]string{
		"leaf": "demo/internal/leaf",
		"std":  "example.com/std",
	}, archive.codegenImports)
	require.Contains(t, archive.goSpecs["example.com/std"].Constraints, "resource.file")
	require.Contains(t, archive.goSpecs["example.com/std"].Defaults, "resource.file")
	require.Contains(t, archive.goSpecs["example.com/std"].Schema.Resources, "file")
	require.NotContains(t, archive.goSpecs["example.com/std"].Constraints, "resource.unused")
	require.NotContains(t, archive.goSpecs["example.com/std"].Defaults, "data-source.query")

	lookup := byName["lookup"]
	require.Equal(t, map[string]string{"std": "example.com/std"}, lookup.codegenImports)
	require.Contains(t, lookup.goSpecs["example.com/std"].Constraints, "data-source.query")
	require.Contains(t, lookup.goSpecs["example.com/std"].Defaults, "data-source.query")
	require.Contains(t, lookup.goSpecs["example.com/std"].Schema.DataSources, "query")
	require.NotContains(t, lookup.goSpecs["example.com/std"].Constraints, "resource.file")

	runtimeLib := runtimeLibraryForCompiledComposites("bundle", composites)
	require.Same(t, nestedLib,
		runtimeLib.Composite(runtime.NodeResource, "archive").Libraries["leaf"])
	require.NotNil(t, runtimeLib.Composite(runtime.NodeDataSource, "lookup").Libraries["std"].Schema)

	imports := codegenImportsForCompiledComposites(composites)
	require.Equal(t, map[string]map[string]map[string]string{
		"resource": {
			"archive": {
				"leaf": "demo/internal/leaf",
				"std":  "example.com/std",
			},
		},
		"data-source": {
			"lookup": {"std": "example.com/std"},
		},
	}, imports)

	goSpecs := goSpecsForCompiledComposites(composites)
	require.Contains(t, goSpecs["example.com/std"].Constraints, "resource.file")
	require.Contains(t, goSpecs["example.com/std"].Constraints, "data-source.query")
	require.Contains(t, goSpecs["example.com/std"].Defaults, "resource.file")
	require.Contains(t, goSpecs["example.com/std"].Defaults, "data-source.query")
	require.Contains(t, goSpecs["example.com/std"].Schema.Resources, "file")
	require.Contains(t, goSpecs["example.com/std"].Schema.DataSources, "query")
	require.NotContains(t, goSpecs["example.com/std"].Schema.Resources, "unused")

	generated, err := codegen.GenerateUBLibraryPackage(
		"bundle",
		"bundle",
		syntaxBodiesForCompiledComposites(composites),
		imports,
		goSpecs,
		nil,
	)
	require.NoError(t, err)
	_, err = parser.ParseFile(token.NewFileSet(), "bundle.go", generated, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", generated)
}

func compositeArtifactLibrary(goSourcePath string) *resolve.UBLibrary {
	resourceBody := syntax.FactoryBody{
		Imports: []syntax.ImportDecl{
			{Alias: syntax.Ident{Name: "std"}, Ref: &lang.StringLit{Value: "example.com/std"}},
			{Alias: syntax.Ident{Name: "leaf"}, Ref: &lang.StringLit{Value: "./leaf"}},
		},
		Resources: []syntax.NodeDecl{{
			Kind: syntax.NodeResource,
			Name: syntax.Ident{Name: "file"},
			Selector: syntax.NodeSelector{
				Alias:  syntax.Ident{Name: "std"},
				Export: syntax.Ident{Name: "file"},
			},
			Body: &lang.ObjectLit{},
		}},
	}
	dataBody := syntax.FactoryBody{
		Imports: []syntax.ImportDecl{
			{Alias: syntax.Ident{Name: "std"}, Ref: &lang.StringLit{Value: "example.com/std"}},
		},
		Data: []syntax.NodeDecl{{
			Kind: syntax.NodeDataSource,
			Name: syntax.Ident{Name: "query"},
			Selector: syntax.NodeSelector{
				Alias:  syntax.Ident{Name: "std"},
				Export: syntax.Ident{Name: "query"},
			},
			Body: &lang.ObjectLit{},
		}},
	}
	return &resolve.UBLibrary{
		SyntaxBodies: map[string]map[string]syntax.FactoryBody{
			"resource":    {"archive": resourceBody},
			"data-source": {"lookup": dataBody},
		},
		BodyImports: map[string]map[string][]resolve.Resolution{
			"resource": {
				"archive": {
					{
						Kind:       resolve.ResolutionGo,
						LocalAlias: "std",
						Path:       "example.com/std",
						SourcePath: goSourcePath,
					},
					{
						Kind:         resolve.ResolutionUB,
						LocalAlias:   "leaf",
						CanonicalKey: "local:/leaf",
					},
				},
			},
			"data-source": {
				"lookup": {{
					Kind:       resolve.ResolutionGo,
					LocalAlias: "std",
					Path:       "example.com/std",
					SourcePath: goSourcePath,
				}},
			},
		},
	}
}

func compositeArtifactSchema() *runtime.LibrarySchema {
	return &runtime.LibrarySchema{
		Resources: map[string]*runtime.TypeSchema{
			"file": {
				SensitiveInputs: []string{"content"},
				Constraints: []lang.ConstraintSpec{{
					Kind:    "predicate",
					Require: "input.content != null",
					Message: "content is required",
				}},
				Defaults: []lang.DefaultSpec{{Field: "input.mode", Value: "420"}},
			},
			"unused": {
				Constraints: []lang.ConstraintSpec{{
					Kind:    "predicate",
					Require: "input.name != null",
					Message: "name is required",
				}},
			},
		},
		DataSources: map[string]*runtime.TypeSchema{
			"query": {
				SensitiveOutputs: []string{"value"},
				Constraints: []lang.ConstraintSpec{{
					Kind:    "predicate",
					Require: "input.name != null",
					Message: "name is required",
				}},
				Defaults: []lang.DefaultSpec{{Field: "input.region", Value: "'us-east-1'"}},
			},
		},
	}
}
