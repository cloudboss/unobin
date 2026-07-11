package gogen

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/filechange"
)

type generationGolden struct {
	Runs    []generationOutputGolden `json:"runs"`
	Partial generationPartialGolden  `json:"partial"`
}

type generationOutputGolden struct {
	OutDir      string              `json:"out-dir"`
	ModulePath  string              `json:"module-path"`
	Resources   int                 `json:"resources"`
	DataSources int                 `json:"data-sources"`
	Files       []filechange.Change `json:"files"`
}

type generationPartialGolden struct {
	Output *generationOutputGolden `json:"output"`
	Error  string                  `json:"error"`
}

func TestGenerateResultGolden(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "library")
	adapter := &mockAdapter{
		name:          "testmod",
		resources:     []ResourceSchema{sampleResourceSchema()},
		dataSources:   []DataSourceSchema{sampleDataSourceSchema()},
		configuration: nil,
	}
	input := Input{OutDir: dir, ModulePath: "example.com/testmod", From: "tf"}
	result := generationGolden{}
	for range 2 {
		output, err := Generate(context.Background(), adapter, input)
		require.NoError(t, err)
		result.Runs = append(result.Runs, generationOutputView(output, dir))
	}
	adapter.resources = nil
	output, err := Generate(context.Background(), adapter, input)
	require.NoError(t, err)
	result.Runs = append(result.Runs, generationOutputView(output, dir))

	failDir := filepath.Join(t.TempDir(), "partial")
	require.NoError(t, os.MkdirAll(filepath.Join(failDir, "go.mod"), 0o755))
	partial, generateErr := Generate(context.Background(), &mockAdapter{
		name: "partial", resources: []ResourceSchema{sampleResourceSchema()},
	}, Input{OutDir: failDir, ModulePath: "example.com/partial", From: "tf"})
	require.Error(t, generateErr)
	result.Partial = generationPartialGolden{
		Output: generationOutputPointer(partial, failDir),
		Error:  strings.ReplaceAll(generateErr.Error(), failDir, "$OUT"),
	}

	body, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	body = append(body, '\n')
	want, err := os.ReadFile("testdata/generation-result.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(body))
}

func generationOutputPointer(output *Output, root string) *generationOutputGolden {
	if output == nil {
		return nil
	}
	view := generationOutputView(output, root)
	return &view
}

func generationOutputView(output *Output, root string) generationOutputGolden {
	files := make([]filechange.Change, len(output.Files))
	for i, change := range output.Files {
		change.Path = strings.TrimPrefix(filepath.ToSlash(change.Path), filepath.ToSlash(root)+"/")
		files[i] = change
	}
	return generationOutputGolden{
		OutDir:      strings.ReplaceAll(filepath.ToSlash(output.OutDir), filepath.ToSlash(root), "$OUT"),
		ModulePath:  output.ModulePath,
		Resources:   output.Resources,
		DataSources: output.DataSources,
		Files:       files,
	}
}

func sampleDataSourceSchema() DataSourceSchema {
	return DataSourceSchema{GoName: "Image", CloudType: "test_image"}
}
