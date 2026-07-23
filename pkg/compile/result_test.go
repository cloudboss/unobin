package compile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/require"
)

type compileResultGolden struct {
	Cases []compileResultCaseGolden `json:"cases"`
}

type compileResultCaseGolden struct {
	Name   string                   `json:"name"`
	Result *compileResultViewGolden `json:"result"`
	Error  string                   `json:"error"`
}

type compileResultViewGolden struct {
	FactoryName     string              `json:"factory-name"`
	Version         string              `json:"version"`
	ContentRevision string              `json:"content-revision"`
	LibraryPath     string              `json:"library-path"`
	SourcePath      string              `json:"source-path"`
	ProjectDir      string              `json:"project-dir"`
	OutputDir       string              `json:"output-dir"`
	MainGoPath      string              `json:"main-go-path"`
	GoModPath       string              `json:"go-mod-path"`
	AssetsPath      string              `json:"assets-path"`
	Built           bool                `json:"built"`
	BinaryPath      string              `json:"binary-path"`
	Files           []filechange.Change `json:"files"`
}

func TestRunResultGolden(t *testing.T) {
	root := compileResultRoot(t)
	options := compileResultOptions(root)
	result := compileResultGolden{}
	compiled, err := RunResult(options)
	result.Cases = append(result.Cases,
		compileResultGoldenCase("created", root, compiled, err))
	compiled, err = RunResult(options)
	result.Cases = append(result.Cases,
		compileResultGoldenCase("unchanged", root, compiled, err))

	blockedRoot := compileResultRoot(t)
	blockedOptions := compileResultOptions(blockedRoot)
	require.NoError(t, os.MkdirAll(filepath.Join(blockedOptions.OutDir, "go.mod"), 0o755))
	compiled, err = RunResult(blockedOptions)
	result.Cases = append(result.Cases,
		compileResultGoldenCase("partial write failure", blockedRoot, compiled, err))

	missingRoot := t.TempDir()
	missingOptions := compileResultOptions(missingRoot)
	compiled, err = RunResult(missingOptions)
	result.Cases = append(result.Cases,
		compileResultGoldenCase("failure before mutation", missingRoot, compiled, err))

	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/run-result.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func TestRunResultWritesCapturedAssets(t *testing.T) {
	root := compileAssetResultRoot(t)
	options := compileResultOptions(root)

	compiled, err := RunResult(options)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "build", "factory.assets"), compiled.AssetsPath)
	require.Contains(t, compiled.Files, filechange.Change{
		Path:   compiled.AssetsPath,
		Action: filechange.ActionCreated,
	})

	bundle, err := os.ReadFile(compiled.AssetsPath)
	require.NoError(t, err)
	catalog, err := asset.Open(bundle)
	require.NoError(t, err)
	require.NoError(t, catalog.Verify())
	require.Len(t, catalog.Sets(), 1)

	mainSource, err := os.ReadFile(compiled.MainGoPath)
	require.NoError(t, err)
	require.Contains(t, string(mainSource), "//go:embed factory.assets")
	require.NotContains(t, string(mainSource), "./message.txt")
}

func TestRunResultRejectsAssetStdout(t *testing.T) {
	root := compileAssetResultRoot(t)
	options := compileResultOptions(root)
	options.OutDir = "-"
	var stdout bytes.Buffer
	options.Stdout = &stdout

	compiled, err := RunResult(options)
	require.Nil(t, compiled)
	require.EqualError(t, err,
		"compile: cannot stream generated source to stdout when assets are declared")
	require.Empty(t, stdout.String())
}

func compileResultRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := ubtest.ReadValidFixture(t, "testdata/ub/run-result", "factory")
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(source), 0o644))
	return root
}

func compileAssetResultRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := ubtest.ReadValidFixture(t, "testdata/ub/assets", "factory")
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(source), 0o644))
	content, err := os.ReadFile("testdata/ub/assets/valid/message.txt")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "message.txt"), content, 0o644))
	return root
}

func compileResultOptions(root string) Options {
	return Options{
		FactoryPath: filepath.Join(root, "factory.ub"),
		OutDir:      filepath.Join(root, "build"), StackName: "demo",
		LibraryPath: "example.com/demo", GoVersion: "1.26",
		Version: "v1.2.3", CLIVersion: "v0.1.0",
		NewResolver: func(string) (resolve.Resolver, error) { return failingResolver{}, nil },
	}
}

func compileResultGoldenCase(
	name string,
	root string,
	result *Result,
	err error,
) compileResultCaseGolden {
	entry := compileResultCaseGolden{Name: name}
	if err != nil {
		entry.Error = filepath.ToSlash(err.Error())
		if root != "" {
			entry.Error = strings.ReplaceAll(entry.Error, filepath.ToSlash(root), "$ROOT")
		}
	}
	if result == nil {
		return entry
	}
	entry.Result = &compileResultViewGolden{
		FactoryName: result.FactoryName, Version: result.Version,
		ContentRevision: result.ContentRevision, LibraryPath: result.LibraryPath,
		SourcePath: compileResultPath(root, result.SourcePath),
		ProjectDir: compileResultPath(root, result.ProjectDir),
		OutputDir:  compileResultPath(root, result.OutputDir),
		MainGoPath: compileResultPath(root, result.MainGoPath),
		GoModPath:  compileResultPath(root, result.GoModPath),
		AssetsPath: compileResultPath(root, result.AssetsPath),
		Built:      result.Built, BinaryPath: compileResultPath(root, result.BinaryPath),
		Files: append([]filechange.Change{}, result.Files...),
	}
	for index := range entry.Result.Files {
		entry.Result.Files[index].Path = compileResultPath(root, entry.Result.Files[index].Path)
	}
	return entry
}

func compileResultPath(root string, path string) string {
	if path == "" {
		return ""
	}
	rel, err := filepath.Rel(root, path)
	if err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}
