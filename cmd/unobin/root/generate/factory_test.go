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

func runFactoryCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetFlags(FactoryCmd)
	root := &cobra.Command{Use: "unobin", SilenceUsage: true}
	root.AddCommand(FactoryCmd)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"factory"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestFactoryWritesStarterFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-factory")

	out, err := runFactoryCmd(t, "-o", dir)
	require.NoError(t, err)
	require.Contains(t, out, filepath.Join(dir, "main.ub"))

	got, err := os.ReadFile(filepath.Join(dir, "main.ub"))
	require.NoError(t, err)
	want := `description: 'TODO: describe this factory'

inputs: {
  # TODO: declare inputs
}

imports: {
  # TODO: declare imports, e.g. std: 'github.com/cloudboss/unobin-library-std'
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

// TestFactoryWritesManifestWithToolchainPin proves a release CLI
// records its own version as the project's unobin pin, so a compile
// by any other version says so up front.
func TestFactoryWritesManifestWithToolchainPin(t *testing.T) {
	prev := CLIVersion
	CLIVersion = func() string { return "v0.3.0" }
	t.Cleanup(func() { CLIVersion = prev })

	dir := filepath.Join(t.TempDir(), "my-factory")
	_, err := runFactoryCmd(t, "-o", dir)
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(dir, "unobin.manifest"))
	require.NoError(t, err)
	require.Equal(t, "unobin: 'v0.3.0'\nrequires: {}\n", string(got))
}

// TestFactoryDevManifestOmitsPin proves a dev build writes the
// manifest without a pin: there is no release version to record.
func TestFactoryDevManifestOmitsPin(t *testing.T) {
	prev := CLIVersion
	CLIVersion = func() string { return "dev" }
	t.Cleanup(func() { CLIVersion = prev })

	dir := filepath.Join(t.TempDir(), "my-factory")
	_, err := runFactoryCmd(t, "-o", dir)
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(dir, "unobin.manifest"))
	require.NoError(t, err)
	require.Equal(t, "requires: {}\n", string(got))
}

func TestFactoryGeneratedFileParsesAndValidates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-factory")
	_, err := runFactoryCmd(t, "-o", dir)
	require.NoError(t, err)

	path := filepath.Join(dir, "main.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	f, err := lang.ParseSource(path, src)
	require.NoError(t, err)
	f.Kind = lang.FileFactory
	errs := lang.ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "validate main.ub: %v", errs.Err())
}

func TestFactoryRefusesExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	_, err := runFactoryCmd(t, "-o", dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestFactoryForceOverwritesExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stale := filepath.Join(dir, "main.ub")
	require.NoError(t, os.WriteFile(stale, []byte("stale content"), 0o644))

	_, err := runFactoryCmd(t, "-o", dir, "--force")
	require.NoError(t, err)

	got, err := os.ReadFile(stale)
	require.NoError(t, err)
	require.Contains(t, string(got), "TODO: describe this factory")
}
