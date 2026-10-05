package gogen

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratePreservesAuthoredFiles(t *testing.T) {
	dir := t.TempDir()
	adapter := &mockAdapter{name: "testmod", resources: []ResourceSchema{sampleResourceSchema()}}
	input := Input{OutDir: dir, ModulePath: "example.com/testmod", From: "tf"}
	_, err := Generate(t.Context(), adapter, input)
	require.NoError(t, err)
	authored := map[string]string{
		"resources/custom_rsrc.go": "package resources\n// authored resource helper\n",
		"configuration.go":         "package testmod\n// authored configuration\n",
	}
	for path, body := range authored {
		require.NoError(t, os.WriteFile(filepath.Join(dir, path), []byte(body), 0o644))
	}
	adapter.resources = nil
	adapter.dataSources = []DataSourceSchema{sampleDataSourceSchema()}
	_, err = Generate(t.Context(), adapter, input)
	require.NoError(t, err)
	for path, want := range authored {
		body, err := os.ReadFile(filepath.Join(dir, path))
		require.NoError(t, err)
		require.Equal(t, want, string(body))
	}
}

func TestGenerateRetainsImplementedLifecycle(t *testing.T) {
	dir := t.TempDir()
	adapter := &mockAdapter{name: "testmod", resources: []ResourceSchema{sampleResourceSchema()}}
	input := Input{OutDir: dir, ModulePath: "example.com/testmod", From: "tf"}
	core, err := filepath.Abs("../..")
	require.NoError(t, err)
	input.ReplaceUnobin = core
	_, err = Generate(t.Context(), adapter, input)
	require.NoError(t, err)
	var implementation string
	for path, body := range generatedTree(t, dir) {
		if strings.Contains(body, "func (r *S3Bucket) Create") {
			implementation = path
		}
	}
	require.NotEmpty(t, implementation)
	path := filepath.Join(dir, implementation)
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	edited := strings.ReplaceAll(string(body), `return nil, fmt.Errorf("create not implemented")`,
		`return &S3BucketOutput{Arn: "implemented"}, nil`)
	require.NotEqual(t, string(body), edited)
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))
	adapter.resources[0].Description = "Updated resource schema"
	_, err = Generate(t.Context(), adapter, input)
	require.NoError(t, err)
	body, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, edited, string(body))
	if testing.Short() {
		return
	}
	consumer, err := os.ReadFile("testdata/go/lifecycle/consumer_test.go")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lifecycle_test.go"), consumer, 0o644))
	for _, args := range [][]string{{"mod", "tidy"}, {"test", "./..."}} {
		command := exec.CommandContext(t.Context(), "go", args...)
		command.Dir = dir
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
	}
}

func TestGenerateRejectsEditedOrCollidingOutputs(t *testing.T) {
	for _, conflict := range []string{"owned", "configuration", "legacy", "obsolete lifecycle"} {
		t.Run(conflict, func(t *testing.T) {
			dir := t.TempDir()
			adapter := &mockAdapter{name: "testmod", resources: []ResourceSchema{sampleResourceSchema()}}
			input := Input{OutDir: dir, ModulePath: "example.com/testmod", From: "tf"}
			if conflict != "legacy" {
				_, err := Generate(t.Context(), adapter, input)
				require.NoError(t, err)
			}
			path := "library.go"
			switch conflict {
			case "configuration":
				path = "configuration.go"
				adapter.configuration = &ConfigurationSchema{
					GoName: "ProviderConfig", Fields: []Field{{Name: "Region", GoType: "string"}},
				}
			case "obsolete lifecycle":
				path = "resources/s3_bucket_impl.go"
				adapter.resources = nil
				adapter.dataSources = []DataSourceSchema{sampleDataSourceSchema()}
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, path), []byte("authored code"), 0o644))
			before := generatedTree(t, dir)
			out, err := Generate(t.Context(), adapter, input)
			require.Error(t, err)
			require.Nil(t, out)
			require.Equal(t, before, generatedTree(t, dir))
		})
	}
}

func TestGeneratePreservesManagedModuleEdits(t *testing.T) {
	dir := t.TempDir()
	adapter := &mockAdapter{name: "testmod", resources: []ResourceSchema{sampleResourceSchema()}}
	input := Input{OutDir: dir, ModulePath: "example.com/testmod", From: "tf"}
	_, err := Generate(t.Context(), adapter, input)
	require.NoError(t, err)
	path := filepath.Join(dir, "go.mod")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	edited := append(body, []byte("\nrequire example.com/author v0.1.0\n")...)
	require.NoError(t, os.WriteFile(path, edited, 0o644))
	_, err = Generate(t.Context(), adapter, input)
	require.NoError(t, err)
	body, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, edited, body)
}

func TestGenerateRenderingFailureLeavesOutputsUntouched(t *testing.T) {
	dir := t.TempDir()
	adapter := &mockAdapter{name: "testmod", resources: []ResourceSchema{sampleResourceSchema()}}
	input := Input{OutDir: dir, ModulePath: "example.com/testmod", From: "tf"}
	_, err := Generate(context.Background(), adapter, input)
	require.NoError(t, err)
	before := generatedTree(t, dir)
	adapter.resources[0].Description = "Changed schema before invalid input"
	adapter.resources = append(adapter.resources, ResourceSchema{
		GoName: "Invalid", InputFields: []Field{{Name: "Broken", GoType: "invalid*", Required: true}},
	})
	_, err = Generate(context.Background(), adapter, input)
	require.Error(t, err)
	require.Equal(t, before, generatedTree(t, dir))
}

func generatedTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		files[rel] = string(body)
		return err
	}))
	return files
}
