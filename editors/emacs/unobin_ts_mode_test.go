package emacs_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnobinTsModeStaticRequirements(t *testing.T) {
	body := readFile(t, "unobin-ts-mode.el")

	require.Contains(t, body, "(provide 'unobin-ts-mode)")
	require.NotContains(t, body, "(define-derived-mode unobin-mode")
	require.Contains(t, body, "(defun unobin-install-treesit-grammar")
	require.Contains(t, body, "(defcustom unobin-treesit-auto-install")
	require.Contains(t, body, "(defcustom unobin-eglot-auto-start")
	require.Contains(t, body, "highlights.scm")
	require.Contains(t, body, ";;; unobin-ts-mode.el")
	require.Contains(t, body, "Package-Requires:")
}

func TestUnobinTsModeReadme(t *testing.T) {
	body := readFile(t, "README.md")

	require.Contains(t, body, "(use-package unobin-ts-mode")
	require.Contains(t, body, ":ensure t")
	require.NotContains(t, body, "unobin-mode")
	require.NotContains(t, body, "load-path")
	require.NotContains(t, body, "eglot-server-programs")
	require.NotContains(t, body, "treesit-language-source-alist")
	require.Contains(t, body, "Manual highlight check")
}

func TestUnobinTsModeByteCompiles(t *testing.T) {
	emacs, err := exec.LookPath("emacs")
	if err != nil {
		t.Skip("emacs not found")
	}
	tmp := t.TempDir()
	source := readFile(t, "unobin-ts-mode.el")
	path := filepath.Join(tmp, "unobin-ts-mode.el")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o644))

	cmd := exec.Command(emacs, "-Q", "--batch", "-f", "batch-byte-compile", path)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestUnobinTsModeUsesCheckoutGrammarRecipe(t *testing.T) {
	emacs, err := exec.LookPath("emacs")
	if err != nil {
		t.Skip("emacs not found")
	}
	cwd, err := os.Getwd()
	require.NoError(t, err)
	modePath := filepath.Join(cwd, "unobin-ts-mode.el")
	grammarDir := filepath.Clean(filepath.Join(cwd, "..", "..", "tree-sitter-unobin"))
	form := fmt.Sprintf(
		`(let* ((recipe (unobin-ts-mode--grammar-recipe))
		        (source (nth 1 recipe)))
		   (unless (equal source %s)
		     (error "expected local grammar source, got %%S" recipe)))`,
		strconv.Quote(grammarDir),
	)

	cmd := exec.Command(emacs, "-Q", "--batch", "--load", modePath, "--eval", form)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}
