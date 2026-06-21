package e2etest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCompiledCase(t *testing.T, cfg config, c CompiledCase) {
	t.Helper()
	if testing.Short() && c.Build {
		t.Skip("skipped: spawns go build")
	}
	workspace := copyCaseToWorkspace(t, c.Dir)
	binary, err := compileCase(t.Context(), cfg.repoRoot, cfg.e2eLibraryDir, c, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if c.Build {
		if _, err := os.Stat(binary); err != nil {
			t.Fatalf("built binary: %v", err)
		}
	}
	if err := seedState(workspace, c); err != nil {
		t.Fatal(err)
	}
	if err := createStateLocks(workspace, c); err != nil {
		t.Fatal(err)
	}
	pinned := map[string]bool{}
	var lastStackPath string
	for _, cmd := range c.Commands {
		if stackPath, ok := stackPathFromArgs(cmd.Args); ok {
			lastStackPath = stackPath
			if shouldPinStack(cmd, stackPath, pinned) {
				pinStack(t, workspace, binary, stackPath)
				pinned[stackPath] = true
			}
		}
		if err := tamperPlanFiles(workspace, cmd.TamperPlanFiles); err != nil {
			t.Fatal(err)
		}
		got, err := runCommand(t.Context(), workspace, binary, cmd)
		if err != nil {
			t.Fatalf("%s: %v", cmd.Name, err)
		}
		got = normalizeCommandResult(got, cfg.repoRoot)
		if err := compareCommandGoldens(c.Dir, cmd, got, *update); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.Files) > 0 {
		files, err := readFileResults(workspace, c.Files)
		if err != nil {
			t.Fatal(err)
		}
		files = normalizeFileResults(files, cfg.repoRoot, workspace)
		if err := compareFileGoldens(c.Dir, c.Files, files, *update); err != nil {
			t.Fatal(err)
		}
	}
	if err := comparePlanSummaries(c.Dir, workspace, c.PlanSummaries, *update); err != nil {
		t.Fatal(err)
	}
	if err := comparePlanEnvelopes(c.Dir, workspace, c.PlanEnvelopes, *update); err != nil {
		t.Fatal(err)
	}
	if err := checkAbsentFiles(workspace, c.AbsentFiles); err != nil {
		t.Fatal(err)
	}
	if c.StateSummary != "" {
		if lastStackPath == "" {
			t.Fatal("state summary requires a command with -c or --config")
		}
		if err := compareStateSummary(c.Dir, workspace, c, lastStackPath, *update); err != nil {
			t.Fatal(err)
		}
	}
}

func pinStack(t *testing.T, workspace, binary, stackPath string) {
	t.Helper()
	cmd := Command{
		Name: "pin",
		Args: []string{"pin", "-c", stackPath},
	}
	got, err := runCommand(t.Context(), workspace, binary, cmd)
	if err != nil {
		t.Fatalf("pin %s: %v", stackPath, err)
	}
	if got.ExitCode != 0 {
		t.Fatalf(
			"pin %s failed with exit code %d\nstdout:\n%s\nstderr:\n%s",
			stackPath,
			got.ExitCode,
			got.Stdout,
			got.Stderr,
		)
	}
}

func stackPathFromArgs(args []string) (string, bool) {
	for i, arg := range args {
		switch {
		case arg == "-c" || arg == "--config":
			if i+1 < len(args) {
				return args[i+1], true
			}
		case strings.HasPrefix(arg, "--config="):
			return strings.TrimPrefix(arg, "--config="), true
		}
	}
	return "", false
}

func shouldPinStack(cmd Command, stackPath string, pinned map[string]bool) bool {
	return stackPath != "" && !cmd.SkipPin && !isPinCommand(cmd) && !pinned[stackPath]
}

func isPinCommand(cmd Command) bool {
	return len(cmd.Args) > 0 && cmd.Args[0] == "pin"
}

func readFileResults(workspace string, checks []FileCheck) (map[string]string, error) {
	out := make(map[string]string, len(checks))
	for _, check := range checks {
		path := filepath.Join(workspace, filepath.FromSlash(check.Path))
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", check.Path, err)
		}
		out[check.Path] = string(body)
	}
	return out, nil
}
