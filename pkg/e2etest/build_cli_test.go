package e2etest

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildUnobinCLIWithoutVCSStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the test CLI")
	}
	repository := e2eRepoRoot(t)
	config := filepath.Join(t.TempDir(), "gitconfig")
	writeText(t, config, "[invalid\n")
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	command := exec.CommandContext(t.Context(), "git", "status", "--porcelain")
	command.Dir = repository
	output, err := command.CombinedOutput()
	var failure *exec.ExitError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, 128, failure.ExitCode(), string(output))
	require.Contains(t, string(output), "bad config")

	binary, err := buildUnobinCLI(t.Context(), repository, t.TempDir())
	require.NoError(t, err)
	output, err = exec.CommandContext(t.Context(), binary, "version").CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "v0.0.0")
}
