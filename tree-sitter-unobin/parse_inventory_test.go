package tree_sitter_unobin_test

import (
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTreeSitterParsesValidUnobinSourceFiles(t *testing.T) {
	npm, err := exec.LookPath("npm")
	if err != nil {
		t.Skip("npm not found")
	}
	files := collectValidUnobinSourceFiles(t)
	require.NotEmpty(t, files)

	const batchSize = 80
	for start := 0; start < len(files); start += batchSize {
		end := min(start+batchSize, len(files))
		args := []string{
			"exec",
			"--package=tree-sitter-cli@0.26.9",
			"--",
			"tree-sitter",
			"parse",
			"--quiet",
		}
		args = append(args, files[start:end]...)
		cmd := exec.Command(npm, args...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
}

func collectValidUnobinSourceFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	repoRoot := filepath.Clean("..")
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if isValidUnobinSourceFile(path) {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)
	return files
}

func isValidUnobinSourceFile(path string) bool {
	if filepath.Ext(path) != ".ub" {
		return false
	}
	slash := filepath.ToSlash(path)
	if strings.HasPrefix(slash, "../internal/ubtest/") ||
		strings.Contains(slash, "/pkg/lang/testdata/ub/types/") ||
		strings.Contains(slash, "/pkg/lang/testdata/ub/format-types/") {
		return false
	}
	return strings.HasPrefix(slash, "../examples/") ||
		(strings.Contains(slash, "/testdata/ub/") && strings.Contains(slash, "/valid/"))
}
