package root

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func runCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
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

func TestVersionPrintsVersion(t *testing.T) {
	prev := Version
	Version = "v1.2.3"
	defer func() { Version = prev }()

	out, err := runCommand(t, "version")
	require.NoError(t, err)
	require.Contains(t, out, "v1.2.3")
}

func TestCompileGeneratesMainGo(t *testing.T) {
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

	out, err := runCommand(t, "compile", "-p", stackPath, "--version", "v0.1.0", "--commit", "abc")
	require.NoError(t, err)

	require.Contains(t, out, "package main")
	require.Contains(t, out, `stackName    = "demo-stack"`)
	require.Contains(t, out, `stackVersion = "v0.1.0"`)
	require.Contains(t, out, `stackCommit  = "abc"`)
	require.Contains(t, out, `"github.com/cloudboss/unobin/pkg/modules/core"`)
}

func TestCompileMissingStackFile(t *testing.T) {
	_, err := runCommand(t, "compile", "-p", "/no/such/path/stack.ub")
	require.Error(t, err)
}

func TestCompileInvalidStackFails(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte("exports: { x: 'y.ub' }\n"), 0o644))

	_, err := runCommand(t, "compile", "-p", stackPath)
	require.Error(t, err)
}
