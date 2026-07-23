package root

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/internal/cmdout"
	compilepkg "github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/stretchr/testify/require"
)

func TestCompileResultFormatGolden(t *testing.T) {
	root := t.TempDir()
	mapper := diagnostic.PathMapper{
		WorkingDir: root, ProjectDir: root,
		Mappings: []diagnostic.PathMapping{
			{AbsoluteRoot: filepath.Join(root, "factory.ub"), DisplayRoot: "factory.ub"},
			{AbsoluteRoot: filepath.Join(root, "build"), DisplayRoot: "build"},
		},
	}
	results := []*compilepkg.Result{
		{
			FactoryName: "demo", Version: "v1.2.3",
			SourcePath: filepath.Join(root, "factory.ub"), ProjectDir: root,
			OutputDir:  filepath.Join(root, "build"),
			MainGoPath: filepath.Join(root, "build", "main.go"),
			GoModPath:  filepath.Join(root, "build", "go.mod"),
			Files: []filechange.Change{
				{Path: filepath.Join(root, "build", "main.go"), Action: filechange.ActionCreated},
				{Path: filepath.Join(root, "build", "go.mod"), Action: filechange.ActionUpdated},
			},
		},
		{
			FactoryName: "demo", Version: "v1.2.3", ContentRevision: "abc123def456",
			LibraryPath: "example.com/demo", SourcePath: filepath.Join(root, "factory.ub"),
			ProjectDir: root, OutputDir: filepath.Join(root, "build"),
			MainGoPath: filepath.Join(root, "build", "main.go"),
			GoModPath:  filepath.Join(root, "build", "go.mod"),
			Built:      true, BinaryPath: filepath.Join(root, "build", "demo"),
			Files: []filechange.Change{
				{Path: filepath.Join(root, "build", "demo"), Action: filechange.ActionCreated},
			},
		},
	}
	diagnostics := []diagnostic.Diagnostic{{
		Code: "unobin.compile.built", Severity: diagnostic.SeverityInfo,
		Message: "Built demo v1.2.3 (content-revision abc123def456)",
	}}
	for _, tc := range []struct {
		format cmdout.Format
		path   string
	}{
		{format: cmdout.FormatJSON, path: "testdata/compile-result.jsonl"},
		{format: cmdout.FormatUnobin, path: "testdata/compile-result-unobin.stdout"},
	} {
		var got bytes.Buffer
		for _, result := range results {
			response, err := buildCompileCommandResult(result, mapper, diagnostics)
			require.NoError(t, err)
			require.NoError(t, cmdout.WriteDocument(&got, tc.format, response))
		}
		want, err := os.ReadFile(tc.path)
		require.NoError(t, err)
		require.Equal(t, string(want), got.String())
	}
}

func TestCompileResultIncludesAssetPath(t *testing.T) {
	root := t.TempDir()
	mapper := diagnostic.PathMapper{
		WorkingDir: root,
		ProjectDir: root,
		Mappings: []diagnostic.PathMapping{{
			AbsoluteRoot: filepath.Join(root, "build"),
			DisplayRoot:  "build",
		}},
	}
	result := &compilepkg.Result{
		FactoryName: "demo",
		SourcePath:  filepath.Join(root, "factory.ub"),
		ProjectDir:  root,
		OutputDir:   filepath.Join(root, "build"),
		MainGoPath:  filepath.Join(root, "build", "main.go"),
		GoModPath:   filepath.Join(root, "build", "go.mod"),
		AssetsPath:  filepath.Join(root, "build", "factory.assets"),
	}

	response, err := buildCompileCommandResult(result, mapper, nil)
	require.NoError(t, err)
	require.NotNil(t, response.Output.Assets)
	require.Equal(t, "build/factory.assets", *response.Output.Assets)
}

type compileToolOutputGolden struct {
	Limit      int                     `json:"limit"`
	Cases      []compileToolOutputCase `json:"cases"`
	Success    []diagnostic.Diagnostic `json:"success"`
	Failure    []diagnostic.Diagnostic `json:"failure"`
	ErrorCodes []string                `json:"error-codes"`
}

type compileToolOutputCase struct {
	Name   string `json:"name"`
	Writes []int  `json:"writes"`
	Output string `json:"output"`
}

func TestCompileToolOutputGolden(t *testing.T) {
	cases := []struct {
		name   string
		limit  int
		chunks []string
	}{
		{name: "fits", limit: 5, chunks: []string{"hello"}},
		{name: "ascii truncation", limit: 5, chunks: []string{"hello!"}},
		{name: "rune truncation", limit: 4, chunks: []string{"ab€"}},
		{name: "chunked truncation", limit: 5, chunks: []string{"ab", "cdef"}},
	}
	result := compileToolOutputGolden{Limit: maxCompileToolOutput}
	for _, tc := range cases {
		buffer := newBoundedToolOutput(tc.limit)
		entry := compileToolOutputCase{Name: tc.name}
		for _, chunk := range tc.chunks {
			written, err := buffer.Write([]byte(chunk))
			require.NoError(t, err)
			entry.Writes = append(entry.Writes, written)
		}
		entry.Output = buffer.String()
		result.Cases = append(result.Cases, entry)
	}

	mapper := diagnostic.PathMapper{Mappings: []diagnostic.PathMapping{
		{AbsoluteRoot: "/tmp/work", DisplayRoot: "build"},
	}}
	stdout := newBoundedToolOutput(50)
	_, err := stdout.Write([]byte("/tmp/work/generated.go\n"))
	require.NoError(t, err)
	stderr := newBoundedToolOutput(50)
	success := &diagnostic.Collector{}
	reportCompileToolOutput(success, mapper, stdout, stderr, false)
	failure := &diagnostic.Collector{}
	reportCompileToolOutput(failure, mapper, stdout, stderr, true)
	result.Success = success.Diagnostics()
	result.Failure = failure.Diagnostics()
	result.ErrorCodes = []string{
		string(compileErrorCode(&os.PathError{Op: "open", Path: "file", Err: os.ErrNotExist})),
		string(compileErrorCode(errors.New("provider failed"))),
	}

	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/compile-tool-output.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}
