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
