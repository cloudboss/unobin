package e2etest

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pmezard/go-difflib/difflib"
)

var update = flag.Bool("update", false, "rewrite e2e golden files from actual output")

// CommandResult stores the process result for one command.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func compareCommandGoldens(caseDir string, cmd Command, got CommandResult, doUpdate bool) error {
	if got.ExitCode != cmd.ExitCode {
		return fmt.Errorf(
			"%s exit code: got %d, want %d\nstdout:\n%s\nstderr:\n%s",
			cmd.Name,
			got.ExitCode,
			cmd.ExitCode,
			got.Stdout,
			got.Stderr,
		)
	}
	if err := compareOptionalGolden(
		caseDir,
		cmd.Name+" stdout",
		cmd.Stdout,
		got.Stdout,
		doUpdate,
	); err != nil {
		return err
	}
	return compareOptionalGolden(caseDir, cmd.Name+" stderr", cmd.Stderr, got.Stderr, doUpdate)
}

func compareFileGoldens(
	caseDir string,
	files []FileCheck,
	got map[string]string,
	doUpdate bool,
) error {
	for _, file := range files {
		content, ok := got[file.Path]
		if !ok {
			return fmt.Errorf("missing file result for %s", file.Path)
		}
		if err := compareOptionalGolden(caseDir, file.Path, file.Want, content, doUpdate); err != nil {
			return err
		}
	}
	return nil
}

func compareOptionalGolden(caseDir, label, relPath, got string, doUpdate bool) error {
	if relPath == "" {
		if got != "" {
			return fmt.Errorf("%s produced output but no golden path is configured", label)
		}
		return nil
	}
	return compareTextGolden(filepath.Join(caseDir, filepath.FromSlash(relPath)), got, doUpdate)
}

func compareTextGolden(path string, got string, doUpdate bool) error {
	if doUpdate {
		return writeOrRemove(path, got)
	}
	want, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if got == "" {
			return nil
		}
		return fmt.Errorf("%s produced output but no golden exists", path)
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if string(want) != got {
		return fmt.Errorf("%s differs from golden\n%s", path, goldenDiff(path, string(want), got))
	}
	return nil
}

func goldenDiff(path string, want string, got string) string {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(want),
		B:        difflib.SplitLines(got),
		FromFile: "golden " + path,
		ToFile:   "actual",
		Context:  3,
	})
	if err != nil {
		return fmt.Sprintf("build diff: %v", err)
	}
	return diff
}

func writeOrRemove(path string, content string) error {
	if content == "" {
		err := os.Remove(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("make golden directory %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func compareCommandResults(first, second CommandResult) error {
	if first.Stdout != second.Stdout {
		return fmt.Errorf("stdout is not deterministic")
	}
	if first.Stderr != second.Stderr {
		return fmt.Errorf("stderr is not deterministic")
	}
	if first.ExitCode != second.ExitCode {
		return fmt.Errorf("exit code is not deterministic")
	}
	return nil
}
