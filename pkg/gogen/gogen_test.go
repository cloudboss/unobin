package gogen

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

type mockAdapter struct {
	name        string
	resources   []ResourceSchema
	dataSources []DataSourceSchema
}

func (m *mockAdapter) Name() string { return m.name }

func (m *mockAdapter) FetchResources(_ context.Context, _ []string) ([]ResourceSchema, error) {
	return m.resources, nil
}

func (m *mockAdapter) FetchDataSources(_ context.Context, _ []string) ([]DataSourceSchema, error) {
	return m.dataSources, nil
}

func TestGenerateWritesFiles(t *testing.T) {
	dir := t.TempDir()
	adapter := &mockAdapter{
		name: "testmod",
		resources: []ResourceSchema{
			sampleResourceSchema(),
		},
	}

	out, err := Generate(context.Background(), adapter, Input{
		OutDir:     dir,
		ModulePath: "example.com/testmod",
		From:       "tf",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if out.Resources != 1 {
		t.Errorf("expected 1 resource, got %d", out.Resources)
	}
	if out.ModulePath != dir {
		t.Errorf("expected ModulePath %q, got %q", dir, out.ModulePath)
	}

	files := []string{
		"resources/s3_bucket_rsrc.go",
		"module.go",
		"go.mod",
	}
	for _, f := range files {
		path := filepath.Join(dir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", path)
		}
	}

	for _, f := range files {
		if filepath.Ext(f) != ".go" {
			continue
		}
		path := filepath.Join(dir, f)
		fset := token.NewFileSet()
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		_, err = parser.ParseFile(fset, path, src, parser.AllErrors)
		if err != nil {
			t.Errorf("%s does not parse: %v\n\n%s", path, err, src)
		}
	}
}

func TestGenerateDefaultOutDir(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	adapter := &mockAdapter{
		name: "testmod",
		resources: []ResourceSchema{
			{
				GoName:    "Foo",
				CloudType: "Test::Foo",
				InputFields: []Field{
					{Name: "Name", GoType: "string", Required: true},
				},
			},
		},
	}

	out, err := Generate(context.Background(), adapter, Input{
		ModulePath: "example.com/testmod",
		From:       "tf",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if out.ModulePath != "./testmod-module" {
		t.Errorf("expected default outDir ./testmod-module, got %q", out.ModulePath)
	}

	_ = os.RemoveAll("./testmod-module")
}

func TestGenerateNoResources(t *testing.T) {
	adapter := &mockAdapter{name: "empty"}
	_, err := Generate(context.Background(), adapter, Input{
		OutDir:     t.TempDir(),
		ModulePath: "example.com/empty",
		From:       "tf",
	})
	if err == nil {
		t.Error("expected error when no resources or data sources found")
	}
}
