package generate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func runUbmoduleCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetFlags(UbmoduleCmd)
	root := &cobra.Command{Use: "unobin", SilenceUsage: true}
	root.AddCommand(UbmoduleCmd)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"ubmodule"}, args...))
	err := root.Execute()
	return out.String(), err
}

func resetFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

func TestUbmoduleDefaultTypeName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "greeter")

	out, err := runUbmoduleCmd(t, "-o", dir)
	require.NoError(t, err)
	require.Contains(t, out, filepath.Join(dir, "module.ub"))
	require.Contains(t, out, filepath.Join(dir, "main.ub"))

	manifest, err := os.ReadFile(filepath.Join(dir, "module.ub"))
	require.NoError(t, err)
	wantManifest := `description: 'TODO: describe this module'

exports: {
  main: 'main.ub'
}
`
	require.Equal(t, wantManifest, string(manifest))

	stub, err := os.ReadFile(filepath.Join(dir, "main.ub"))
	require.NoError(t, err)
	wantStub := `description: 'TODO: describe this composite type'

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
	require.Equal(t, wantStub, string(stub))
}

func TestUbmoduleCustomTypeName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "greeter")

	_, err := runUbmoduleCmd(t, "-o", dir, "--type", "greeting")
	require.NoError(t, err)

	manifest, err := os.ReadFile(filepath.Join(dir, "module.ub"))
	require.NoError(t, err)
	require.Contains(t, string(manifest), "greeting: 'greeting.ub'")

	_, err = os.Stat(filepath.Join(dir, "greeting.ub"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "main.ub"))
	require.True(t, os.IsNotExist(err), "main.ub should not exist when --type=greeting")
}

func TestUbmoduleGeneratedFilesParseAndValidate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "greeter")
	_, err := runUbmoduleCmd(t, "-o", dir)
	require.NoError(t, err)

	cases := []struct {
		name string
		kind lang.FileKind
	}{
		{"module.ub", lang.FileModule},
		{"main.ub", lang.FileExportedType},
	}
	for _, tc := range cases {
		path := filepath.Join(dir, tc.name)
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		f, err := lang.ParseSource(path, src)
		require.NoError(t, err, "parse %s", tc.name)
		f.Kind = tc.kind
		errs := lang.ValidateFile(f)
		require.Equal(t, 0, errs.Len(), "validate %s: %v", tc.name, errs.Err())
	}
}

func TestUbmoduleRefusesExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "greeter")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	_, err := runUbmoduleCmd(t, "-o", dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestUbmoduleForceOverwritesExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "greeter")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stale := filepath.Join(dir, "module.ub")
	require.NoError(t, os.WriteFile(stale, []byte("stale content"), 0o644))

	_, err := runUbmoduleCmd(t, "-o", dir, "--force")
	require.NoError(t, err)

	manifest, err := os.ReadFile(stale)
	require.NoError(t, err)
	require.Contains(t, string(manifest), "main: 'main.ub'")
}
