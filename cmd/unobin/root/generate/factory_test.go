package generate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/deps"
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
