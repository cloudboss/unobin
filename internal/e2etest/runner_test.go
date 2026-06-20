package e2etest

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommandRunnerCopiesWorkspaceAndCapturesOutput(t *testing.T) {
	caseDir := t.TempDir()
	writeText(t, filepath.Join(caseDir, "files/input.txt"), "hello\n")
	stdout := "input=hello\nargBase=input.txt\nenvBase=input.txt\ncwd=run\n"
	writeText(t, filepath.Join(caseDir, "want/helper.stdout"), stdout)
	writeText(t, filepath.Join(caseDir, "want/helper.stderr"), "from stderr\n")

	workspace := copyCaseToWorkspace(t, caseDir)
	require.FileExists(t, filepath.Join(workspace, "files/input.txt"))
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, "run"), 0o755))

	exe, err := filepath.Abs(os.Args[0])
	require.NoError(t, err)
	cmd := Command{
		Name: "helper",
		Args: []string{
			"-test.run=TestCommandRunnerHelper",
			"--",
			"$WORKSPACE/files/input.txt",
		},
		Dir: "run",
		Env: map[string]string{
			"E2ETEST_ENV_PATH":  "$WORKSPACE/files/input.txt",
			"E2ETEST_EXIT_CODE": "7",
			"E2ETEST_HELPER":    "1",
		},
		Stdout:   "want/helper.stdout",
		Stderr:   "want/helper.stderr",
		ExitCode: 7,
	}

	got, err := runCommand(t.Context(), workspace, exe, cmd)

	require.NoError(t, err)
	require.NoError(t, compareCommandGoldens(caseDir, cmd, got, *update))
}

func TestCommandRunnerRejectsBadWorkingDir(t *testing.T) {
	_, err := runCommand(t.Context(), t.TempDir(), os.Args[0], Command{
		Name: "bad-dir",
		Dir:  "../outside",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "dir must stay under the case directory")
}

func TestCommandRunnerHelper(t *testing.T) {
	if os.Getenv("E2ETEST_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "missing helper path")
		os.Exit(2)
	}
	path := args[1]
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read helper input: %v\n", err)
		os.Exit(2)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "get cwd: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("input=%s", content)
	fmt.Printf("argBase=%s\n", filepath.Base(path))
	fmt.Printf("envBase=%s\n", filepath.Base(os.Getenv("E2ETEST_ENV_PATH")))
	fmt.Printf("cwd=%s\n", filepath.Base(cwd))
	fmt.Fprintln(os.Stderr, "from stderr")
	code, err := strconv.Atoi(os.Getenv("E2ETEST_EXIT_CODE"))
	if err != nil {
		os.Exit(2)
	}
	os.Exit(code)
}

func writeText(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
