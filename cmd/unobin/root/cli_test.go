package root

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func runCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetFlags(CompileCmd)
	root := &cobra.Command{
		Use:           "unobin",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(VersionCmd)
	root.AddCommand(CompileCmd)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func resetFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

func TestVersionPrintsVersion(t *testing.T) {
	prev := Version
	Version = "v1.2.3"
	defer func() { Version = prev }()

	out, err := runCommand(t, "version")
	require.NoError(t, err)
	require.Contains(t, out, "v1.2.3")
}

func TestCompileToStdout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	src := `
imports: {
  core: 'github.com/cloudboss/unobin/pkg/modules/core@v0.1.0'
}
actions: {
  core: { command: { hi: { argv: ['echo', 'hi'] } } }
}
`
	require.NoError(t, os.WriteFile(stackPath, []byte(src), 0o644))

	out, err := runCommand(t, "compile", "-p", stackPath, "-o", "-",
		"--version", "v0.1.0", "--commit", "abc")
	require.NoError(t, err)

	require.Contains(t, out, "package main")
	require.Contains(t, out, `stackName    = "demo-stack"`)
	require.Contains(t, out, `stackVersion = "v0.1.0"`)
	require.Contains(t, out, `stackCommit  = "abc"`)
	require.Contains(t, out, `"github.com/cloudboss/unobin/pkg/modules/core"`)
}

func TestCompileWriteOut(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	src := `
imports: {
  core: 'github.com/cloudboss/unobin/pkg/modules/core@v0.1.0'
}
`
	require.NoError(t, os.WriteFile(stackPath, []byte(src), 0o644))

	outDir := filepath.Join(t.TempDir(), "build")
	_, err := runCommand(t, "compile", "-p", stackPath, "-o", outDir,
		"--unobin-version", "v0.1.0")
	require.NoError(t, err)

	mainBytes, err := os.ReadFile(filepath.Join(outDir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(mainBytes), "package main")

	modBytes, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(modBytes), "module demo-stack")
	require.Contains(t, string(modBytes), "github.com/cloudboss/unobin v0.1.0")
}

func TestCompileRequiresOut(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte("description: 'x'"), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out")
}

func TestCompileMissingStackFile(t *testing.T) {
	_, err := runCommand(t, "compile", "-p", "/no/such/path/stack.ub", "-o", "-")
	require.Error(t, err)
}

func TestCompileInvalidStackFails(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte("exports: { x: 'y.ub' }\n"), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath, "-o", "-")
	require.Error(t, err)
}
