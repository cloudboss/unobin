package e2etest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileCaseBuildsBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped: spawns go build")
	}
	cases, err := DiscoverCompiledCases(compiledFixtureDir(t))
	require.NoError(t, err)
	require.Len(t, cases, 1)
	c := cases[0]

	repoRoot := e2eRepoRoot(t)
	workspace := copyCaseToWorkspace(t, c.Dir)
	binary, err := compileCase(t.Context(), repoRoot, e2eLibraryDir(t), c, workspace)
	require.NoError(t, err)
	require.FileExists(t, binary)

	cmd := c.Commands[0]
	got, err := runCommand(t.Context(), workspace, binary, cmd)
	require.NoError(t, err)
	got = normalizeCommandResult(got, repoRoot)
	require.NoError(t, compareCommandGoldens(c.Dir, cmd, got, *update))
}

func compiledFixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(e2eRepoRoot(t), "internal", "e2etest", "testdata", "ub", "valid", "compiled")
}
