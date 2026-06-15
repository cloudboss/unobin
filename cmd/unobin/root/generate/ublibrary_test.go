package generate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func runUblibraryCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetFlags(UblibraryCmd)
	root := &cobra.Command{Use: "unobin", SilenceUsage: true}
	root.AddCommand(UblibraryCmd)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"ublibrary"}, args...))
	err := root.Execute()
	return out.String(), err
}

func resetFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

func TestUblibraryDefaultTypeName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "greeter")

	out, err := runUblibraryCmd(t, "-o", dir)
	require.NoError(t, err)
	require.Contains(t, out, filepath.Join(dir, "example.ub"))

	stub, err := os.ReadFile(filepath.Join(dir, "example.ub"))
	require.NoError(t, err)
	wantStub := `example: resource {
  description: 'TODO: describe this composite type'
  inputs:      {}
  imports:     {}
  data:        {}
  resources:   {}
  actions:     {}
  outputs:     {}
}
`
	require.Equal(t, wantStub, string(stub))
}

func TestUblibraryCustomTypeName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "greeter")

	_, err := runUblibraryCmd(t, "-o", dir, "--type", "greeting")
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "greeting.ub"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "example.ub"))
	require.True(t, os.IsNotExist(err), "example.ub should not exist when --type=greeting")
}

func TestUblibraryGeneratedFilesParseAndValidate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "greeter")
	_, err := runUblibraryCmd(t, "-o", dir)
	require.NoError(t, err)

	path := filepath.Join(dir, "example.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	f, err := syntax.ParseSource(path, src)
	require.NoError(t, err)
	errs := syntax.ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "validate: %v", errs.Err())
}

func TestUblibraryRefusesReservedTypeName(t *testing.T) {
	for _, name := range []string{"factory", "lock", "main", "manifest"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "greeter")
			_, err := runUblibraryCmd(t, "-o", dir, "--type", name)
			require.Error(t, err)
			require.Contains(t, err.Error(), "reserved")
		})
	}
}

func TestUblibraryRefusesPathTypeName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "greeter")
	_, err := runUblibraryCmd(t, "-o", dir, "--type", "nested/name")
	require.Error(t, err)
	require.Contains(t, err.Error(), "file name")
}

func TestUblibraryRefusesExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "greeter")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	_, err := runUblibraryCmd(t, "-o", dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestUblibraryForceOverwritesExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "greeter")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stale := filepath.Join(dir, "example.ub")
	require.NoError(t, os.WriteFile(stale, []byte("stale content"), 0o644))

	_, err := runUblibraryCmd(t, "-o", dir, "--force")
	require.NoError(t, err)

	stub, err := os.ReadFile(stale)
	require.NoError(t, err)
	require.Contains(t, string(stub), "TODO: describe this composite type")
}
