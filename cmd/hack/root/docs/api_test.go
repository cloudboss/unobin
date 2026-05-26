package docs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIWritesReference(t *testing.T) {
	mod := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(mod, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.26\n"), 0o644))
	pkgDir := filepath.Join(mod, "pkg", "greeter")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "greeter.go"),
		[]byte("// Package greeter greets people.\n"+
			"package greeter\n\n"+
			"// Hello returns a greeting.\n"+
			"func Hello() string { return \"hi\" }\n"), 0o644))

	t.Chdir(mod)

	outDir := filepath.Join(mod, "out")
	out := &bytes.Buffer{}
	APICmd.SetOut(out)
	APICmd.SetErr(out)
	APICmd.SetArgs([]string{"-o", outDir, "pkg"})
	require.NoError(t, APICmd.Execute())

	content, err := os.ReadFile(filepath.Join(outDir, "api", "pkg", "greeter.md"))
	require.NoError(t, err)
	got := string(content)
	require.Contains(t, got, "# package greeter")
	require.Contains(t, got, "Package greeter greets people.")
	require.Contains(t, got, "### func Hello")
	require.Contains(t, got, "func Hello() string")

	summary, err := os.ReadFile(filepath.Join(outDir, "api", "SUMMARY.md"))
	require.NoError(t, err)
	require.Equal(t, "* [pkg/greeter](pkg/greeter.md)\n", string(summary))
}
