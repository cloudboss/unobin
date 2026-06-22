package e2etest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func copyCaseToWorkspace(t testing.TB, caseDir string) string {
	t.Helper()
	workspace := t.TempDir()
	if err := copyTree(caseDir, workspace); err != nil {
		t.Fatalf("copy case to workspace: %v", err)
	}
	return workspace
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if mode.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if d.IsDir() {
			return os.MkdirAll(target, mode.Perm())
		}
		if !mode.IsRegular() {
			return fmt.Errorf("unsupported fixture file mode %s for %s", mode, path)
		}
		return copyFile(path, target, mode.Perm())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	return errors.Join(copyErr, closeErr)
}

func runCommand(
	ctx context.Context,
	workspace string,
	executable string,
	cmd Command,
) (CommandResult, error) {
	dir, err := commandDir(workspace, cmd.Dir)
	if err != nil {
		return CommandResult{}, err
	}
	args := expandValues(workspace, cmd.Args)
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = dir
	command.Env = commandEnv(workspace, cmd.Env)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	runErr := command.Run()
	result := CommandResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if runErr == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return result, fmt.Errorf("run %s: %w", cmd.Name, runErr)
}

func commandDir(workspace, rel string) (string, error) {
	if rel == "" {
		return workspace, nil
	}
	if err := checkRelPath("dir", rel); err != nil {
		return "", err
	}
	return filepath.Join(workspace, filepath.FromSlash(rel)), nil
}

func commandEnv(workspace string, env map[string]string) []string {
	out := append([]string{}, os.Environ()...)
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, key+"="+expandValue(workspace, env[key]))
	}
	return out
}

func expandValues(workspace string, values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, expandValue(workspace, value))
	}
	return out
}

func expandValue(workspace, value string) string {
	return os.Expand(value, func(name string) string {
		if name == "WORKSPACE" {
			return workspace
		}
		return "$" + name
	})
}
