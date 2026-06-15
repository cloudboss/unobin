package generate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
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
	require.Contains(t, out, filepath.Join(dir, "factory.ub"))

	got, err := os.ReadFile(filepath.Join(dir, "factory.ub"))
	require.NoError(t, err)
	want := `factory: {
  description: 'TODO: describe this factory'
  inputs:      {}
  imports:     {}
  data:        {}
  resources:   {}
  actions:     {}
  outputs:     {}
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

	got, err := os.ReadFile(filepath.Join(dir, "manifest.ub"))
	require.NoError(t, err)
	want := `manifest: {
  unobin-version: 'v0.3.0'
  requires:       {}
}
`
	require.Equal(t, want, string(got))
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

	got, err := os.ReadFile(filepath.Join(dir, "manifest.ub"))
	require.NoError(t, err)
	want := `manifest: {
  requires: {}
}
`
	require.Equal(t, want, string(got))
}

func TestFactoryGeneratedFileParsesAndValidates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-factory")
	_, err := runFactoryCmd(t, "-o", dir)
	require.NoError(t, err)

	path := filepath.Join(dir, "factory.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	f, err := syntax.ParseSource(path, src)
	require.NoError(t, err)
	errs := syntax.ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "validate factory.ub: %v", errs.Err())
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
	stale := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stale, []byte("stale content"), 0o644))
	staleManifest := filepath.Join(dir, deps.ManifestFileName)
	require.NoError(t, os.WriteFile(staleManifest, []byte("stale content"), 0o644))

	_, err := runFactoryCmd(t, "-o", dir, "--force")
	require.NoError(t, err)

	got, err := os.ReadFile(stale)
	require.NoError(t, err)
	require.Contains(t, string(got), "TODO: describe this factory")
	manifest, err := os.ReadFile(staleManifest)
	require.NoError(t, err)
	require.Contains(t, string(manifest), "manifest:")
}
