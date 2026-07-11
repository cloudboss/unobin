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
	want, err := os.ReadFile(filepath.Join("testdata", "cli.md"))
	require.NoError(t, err)
	require.Equal(t, string(want), string(content))
}
