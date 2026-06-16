package docs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCLIWritesReference(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "reference")
	out := &bytes.Buffer{}
	CLICmd.SetOut(out)
	CLICmd.SetErr(out)
	CLICmd.SetArgs([]string{"-o", outDir})
	require.NoError(t, CLICmd.Execute())

	content, err := os.ReadFile(filepath.Join(outDir, "cli.md"))
	require.NoError(t, err)
	got := string(content)
	require.Contains(t, got, "# CLI reference")
	require.Contains(t, got, "## unobin compile")
	require.Contains(t, got, "## unobin version")
	require.Contains(t, got, "factory.ub")
	require.Contains(t, got, "manifest.ub")
	require.Contains(t, got, "lock.ub")
	require.Contains(t, got, "stack file")
	require.NotContains(t, got, "--help", "cobra's auto help flag must not appear")
}
