package e2etest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunSourceCasesRunsFromRootAndComparesFiles(t *testing.T) {
	caseRoot := t.TempDir()
	caseDir := filepath.Join(caseRoot, "source")
	writeText(t, filepath.Join(caseDir, "case.json"), `{
		"name": "source",
		"rootPath": "root",
		"commands": [
			{
				"name": "source-helper",
				"args": [
					"-test.run=TestSourceCaseHelper",
					"--",
					"$WORKSPACE/root/generated.txt"
				],
				"stdout": "want/source-helper.stdout",
				"stderr": "want/source-helper.stderr",
				"env": { "E2ETEST_SOURCE_HELPER": "1" }
			}
		],
		"files": [
			{ "path": "root/generated.txt", "want": "want/generated.txt" }
		]
	}`)
	writeText(t, filepath.Join(caseDir, "root/input.txt"), "input\n")
	writeText(t, filepath.Join(caseDir, "want/source-helper.stdout"), "cwd=root\n")
	writeText(t, filepath.Join(caseDir, "want/source-helper.stderr"), "wrote generated.txt\n")
	writeText(t, filepath.Join(caseDir, "want/generated.txt"), "generated from root\n")

	RunSourceCases(t, caseRoot,
		WithUnobinExecutable(os.Args[0]),
		WithE2ELibraryDir(""),
	)
}

func TestCheckAbsentFiles(t *testing.T) {
	workspace := t.TempDir()
	writeText(t, filepath.Join(workspace, "present.txt"), "x")

	require.NoError(t, checkAbsentFiles(workspace, []string{"missing.txt"}))
	err := checkAbsentFiles(workspace, []string{"present.txt"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "present.txt exists")
}

func TestSourceRemoteMapSetsProjectMetadata(t *testing.T) {
	workspace := t.TempDir()
	writeText(t, filepath.Join(workspace, "repo/manifest.ub"),
		readSourceFixture(t, "manifest.ub"))
	writeText(t, filepath.Join(workspace, "repo/lib/library.ub"),
		readSourceFixture(t, "library.ub"))

	remotes, err := sourceRemoteMap(workspace, []RemoteSource{
		{
			Key:            "example.com/repo//lib@v1.0.0",
			Path:           "repo/lib",
			ProjectPath:    "repo",
			ProjectSubdir:  "",
			PackageSubdir:  "lib",
			ModuleRootPath: "repo",
			ModulePath:     "example.com/repo",
			GoImportPath:   "example.com/repo/lib",
		},
	})
	require.NoError(t, err)

	src := remotes["example.com/repo//lib@v1.0.0"]
	require.NotNil(t, src.ProjectFS)
	require.Equal(t, filepath.Join(workspace, "repo"), src.ProjectPath)
	require.Equal(t, "", src.ProjectSubdir)
	require.Equal(t, "lib", src.PackageSubdir)
	require.Equal(t, filepath.Join(workspace, "repo"), src.ModuleRootPath)
	require.Equal(t, "example.com/repo", src.ModulePath)
	require.Equal(t, "example.com/repo/lib", src.GoImportPath)
}

func readSourceFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", "ub", "source-remote", "valid", name)
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

func TestRootCommandRunnerSetsCLIVersion(t *testing.T) {
	workspace := t.TempDir()
	runRoot, cleanup, err := rootCommandRunner(workspace, SourceCase{
		Executor:   "root",
		CLIVersion: "v9.9.9",
	})
	require.NoError(t, err)
	defer cleanup()

	got, err := runRoot(t.Context(), workspace, Command{Name: "version", Args: []string{"version"}})
	require.NoError(t, err)
	require.Contains(t, got.Stdout, "v9.9.9")
}

func TestRootCommandRunnerUsesWorkspaceImportCache(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("HOME", filepath.Join(workspace, "home"))
	t.Setenv("XDG_CACHE_HOME", "")
	runRoot, cleanup, err := rootCommandRunner(workspace, SourceCase{Executor: "root"})
	require.NoError(t, err)
	defer cleanup()

	got, err := runRoot(t.Context(), workspace, Command{
		Name: "deps-clean",
		Args: []string{"deps", "clean"},
	})
	require.NoError(t, err)

	want := "Removed the import cache at " +
		filepath.Join(workspace, "cache", "unobin", "imports") + "\n"
	require.Equal(t, want, got.Stderr)
	require.Empty(t, got.Stdout)
	require.Zero(t, got.ExitCode)
}

func TestSourceCaseHelper(t *testing.T) {
	if os.Getenv("E2ETEST_SOURCE_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "missing output path")
		os.Exit(2)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "get cwd: %v\n", err)
		os.Exit(2)
	}
	require.NoError(t, os.WriteFile(args[1], []byte("generated from root\n"), 0o644))
	fmt.Printf("cwd=%s\n", filepath.Base(cwd))
	fmt.Fprintln(os.Stderr, "wrote generated.txt")
	os.Exit(0)
}
