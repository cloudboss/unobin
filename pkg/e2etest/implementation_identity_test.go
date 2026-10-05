package e2etest

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalImplementationChangesRejectSavedPlan(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped: spawns go build")
	}
	c := CompiledCase{
		Name: "implementation-identity", FactoryPath: "src", Build: true,
		LibraryPath: "example.com/identity/factory",
	}
	workspace := copyCaseToWorkspace(t, "testdata/ub/valid/implementation-identity")
	cfg, err := newConfig(t.Context(), []Option{
		WithUnobinDir(e2eRepoRoot(t)),
		WithGoModule("example.com/identity/helper", filepath.Join(workspace, "src/modules/helper")),
	})
	require.NoError(t, err)
	binary, err := compileCase(cfg, c, workspace)
	require.NoError(t, err)
	pinStack(t, workspace, binary, "stacks/dev.ub", cfg)
	got, err := runCommand(t.Context(), workspace, binary, cfg.command(Command{
		Name: "plan", Args: []string{"plan", "-c", "stacks/dev.ub", "-o", "saved.ubp"},
	}))
	require.NoError(t, err)
	require.Zero(t, got.ExitCode, got.Stderr)
	before := identityStateFiles(t, workspace)

	for _, path := range []string{"src/modules/library/marker.go", "src/modules/helper/helper.go"} {
		t.Run(path, func(t *testing.T) {
			original, err := os.ReadFile(filepath.Join(workspace, path))
			require.NoError(t, err)
			require.NoError(t, replaceCaseFile(workspace, path, "mutations/"+filepath.Base(path)))
			t.Cleanup(func() {
				require.NoError(t, os.WriteFile(filepath.Join(workspace, path), original, 0o644))
			})
			rebuilt, err := compileCaseTo(cfg, c, workspace,
				filepath.Join(workspace, ".e2e", "rebuild"), true)
			require.NoError(t, err)
			got, err := runCommand(t.Context(), workspace, rebuilt, cfg.command(Command{
				Name: "apply", Args: []string{"apply", "saved.ubp"},
			}))
			require.NoError(t, err)
			require.Equal(t, 1, got.ExitCode, got.Stdout+got.Stderr)
			require.Contains(t, got.Stderr, "plan was computed for")
			require.NoFileExists(t, filepath.Join(workspace, "lifecycle.txt"))
			require.Equal(t, before, identityStateFiles(t, workspace))
		})
	}

	binary, err = compileCaseTo(cfg, c, workspace,
		filepath.Join(workspace, ".e2e", "unchanged"), true)
	require.NoError(t, err)
	got, err = runCommand(t.Context(), workspace, binary, cfg.command(Command{
		Name: "apply", Args: []string{"apply", "saved.ubp"},
	}))
	require.NoError(t, err)
	require.Zero(t, got.ExitCode, got.Stderr)
	body, err := os.ReadFile(filepath.Join(workspace, "lifecycle.txt"))
	require.NoError(t, err)
	require.Equal(t, "baseline", string(body))
}

func identityStateFiles(t *testing.T, workspace string) map[string]string {
	t.Helper()
	dir := filepath.Join(workspace, ".unobin", "state")
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err == nil {
			files[rel] = string(body)
		}
		return err
	})
	require.NoError(t, err)
	return files
}
