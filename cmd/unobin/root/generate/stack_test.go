package generate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func runStackCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetFlags(StackCmd)
	root := &cobra.Command{Use: "unobin", SilenceUsage: true}
	root.AddCommand(StackCmd)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"stack"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestStackWritesStarterFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-stack")

	out, err := runStackCmd(t, "-o", dir)
	require.NoError(t, err)
	require.Contains(t, out, filepath.Join(dir, "stack.ub"))

	got, err := os.ReadFile(filepath.Join(dir, "stack.ub"))
	require.NoError(t, err)
	want := `description: 'TODO: describe this stack'

inputs: {
  # TODO: declare inputs
}

imports: {
  # TODO: declare imports
}

resources: {
  # TODO: declare resources
}

outputs: {
  # TODO: declare outputs
}
`
	require.Equal(t, want, string(got))
}

func TestStackGeneratedFileParsesAndValidates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-stack")
	_, err := runStackCmd(t, "-o", dir)
	require.NoError(t, err)

	path := filepath.Join(dir, "stack.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	f, err := lang.ParseSource(path, src)
	require.NoError(t, err)
	f.Kind = lang.FileStack
	errs := lang.ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "validate stack.ub: %v", errs.Err())
}

func TestStackRefusesExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	_, err := runStackCmd(t, "-o", dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestStackForceOverwritesExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-stack")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stale := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stale, []byte("stale content"), 0o644))

	_, err := runStackCmd(t, "-o", dir, "--force")
	require.NoError(t, err)

	got, err := os.ReadFile(stale)
	require.NoError(t, err)
	require.Contains(t, string(got), "TODO: describe this stack")
}
