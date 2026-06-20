package e2etest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runSourceCase(t *testing.T, cfg config, executable string, c SourceCase) {
	t.Helper()
	workspace := copyCaseToWorkspace(t, c.Dir)
	if err := copySourceModules(workspace, cfg.e2eLibraryDir); err != nil {
		t.Fatal(err)
	}
	expansions := map[string]string{
		"WORKSPACE":       workspace,
		"REPO_ROOT":       cfg.repoRoot,
		"E2E_LIBRARY_DIR": cfg.e2eLibraryDir,
	}
	for _, cmd := range c.Commands {
		cmd = sourceCommand(c, cmd)
		cmd = expandCommand(cmd, expansions)
		got, err := runCommand(t.Context(), workspace, executable, cmd)
		if err != nil {
			t.Fatalf("%s: %v", cmd.Name, err)
		}
		got = normalizeCommandResult(got, cfg.repoRoot)
		if err := compareCommandGoldens(c.Dir, cmd, got, *update); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.Files) == 0 {
		return
	}
	files, err := readFileResults(workspace, c.Files)
	if err != nil {
		t.Fatal(err)
	}
	if err := compareFileGoldens(c.Dir, c.Files, files, *update); err != nil {
		t.Fatal(err)
	}
}

func sourceCommand(c SourceCase, cmd Command) Command {
	if cmd.Dir == "" {
		cmd.Dir = c.RootPath
	}
	return cmd
}

func copySourceModules(workspace string, e2eLibraryDir string) error {
	if e2eLibraryDir == "" {
		return nil
	}
	target := filepath.Join(workspace, "modules", "e2elib")
	_, err := os.Stat(target)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat source module target: %w", err)
	}
	return copyTree(e2eLibraryDir, target)
}

func expandCommand(cmd Command, values map[string]string) Command {
	cmd.Args = expandWithValues(cmd.Args, values)
	cmd.Dir = expandStringWithValues(cmd.Dir, values)
	if len(cmd.Env) == 0 {
		return cmd
	}
	env := make(map[string]string, len(cmd.Env))
	for key, value := range cmd.Env {
		env[key] = expandStringWithValues(value, values)
	}
	cmd.Env = env
	return cmd
}

func expandWithValues(in []string, values map[string]string) []string {
	out := make([]string, 0, len(in))
	for _, value := range in {
		out = append(out, expandStringWithValues(value, values))
	}
	return out
}

func expandStringWithValues(value string, values map[string]string) string {
	return os.Expand(value, func(name string) string {
		if value, ok := values[name]; ok {
			return value
		}
		return "$" + name
	})
}

func buildUnobinCLI(ctx context.Context, repoRoot string, outDir string) (string, error) {
	binary := filepath.Join(outDir, "unobin")
	ldflags := "-X github.com/cloudboss/unobin/cmd/unobin/root.Version=v0.0.0"
	cmd := exec.CommandContext(
		ctx,
		"go",
		"build",
		"-ldflags",
		ldflags,
		"-o",
		binary,
		"./cmd/unobin",
	)
	cmd.Dir = repoRoot
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf(
			"build unobin CLI: %w\nstdout:\n%s\nstderr:\n%s",
			err,
			stdout.String(),
			stderr.String(),
		)
	}
	return binary, nil
}
