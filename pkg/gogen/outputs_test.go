package gogen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderOutputFilesSplitsLifecycle(t *testing.T) {
	files, err := renderOutputFiles("testmod", Input{ModulePath: "example.com/testmod", From: "tf"},
		[]ResourceSchema{sampleResourceSchema()}, []DataSourceSchema{sampleDataSourceSchema()}, nil)
	require.NoError(t, err)
	var owned, preserved []string
	for _, file := range files {
		if file.Preserve {
			preserved = append(preserved, file.Path)
		} else {
			owned = append(owned, file.Path)
		}
	}
	require.ElementsMatch(t, []string{
		"resources/s3_bucket_rsrc.go", "data/image_dsrc.go", "library.go",
	}, owned)
	require.ElementsMatch(t, []string{
		"resources/s3_bucket_impl.go", "data/image_impl.go", "go.mod",
	}, preserved)
}

func TestSplitLifecycleFileKeepsDeclarationsAndMethods(t *testing.T) {
	resource, err := ResourceFile(sampleResourceSchema(), "tf")
	require.NoError(t, err)
	data, err := DataSourceFile(sampleDataSourceSchema(), "tf")
	require.NoError(t, err)
	for _, source := range [][]byte{resource, data} {
		declarations, methods, err := splitLifecycleFile(source)
		require.NoError(t, err)
		declFile, err := parser.ParseFile(token.NewFileSet(), "types.go", declarations, 0)
		require.NoError(t, err)
		implFile, err := parser.ParseFile(token.NewFileSet(), "implementation.go", methods, 0)
		require.NoError(t, err)
		var typeNames, methodNames []string
		for _, decl := range declFile.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				require.Nil(t, fn.Recv)
			}
			if decl, ok := decl.(*ast.GenDecl); ok && decl.Tok == token.TYPE {
				for _, spec := range decl.Specs {
					typeNames = append(typeNames, spec.(*ast.TypeSpec).Name.Name)
				}
			}
		}
		for _, decl := range implFile.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				require.NotNil(t, fn.Recv)
				methodNames = append(methodNames, fn.Name.Name)
			} else {
				require.Equal(t, token.IMPORT, decl.(*ast.GenDecl).Tok)
			}
		}
		if string(source) == string(resource) {
			require.Equal(t, []string{"S3Bucket", "S3BucketOutput"}, typeNames)
			require.Equal(t, []string{"Create", "Read", "Update", "Delete"}, methodNames)
		} else {
			require.Equal(t, []string{"Image", "ImageOutput"}, typeNames)
			require.Equal(t, []string{"Read"}, methodNames)
		}
	}
}
