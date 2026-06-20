package e2etest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeWorkspaceResult(t *testing.T) {
	got := normalizeWorkspaceResult(CommandResult{
		Stdout: "/tmp/work/cache\n",
		Stderr: "removed /tmp/work/cache\n",
	}, "/tmp/work")

	require.Equal(t, "<workspace>/cache\n", got.Stdout)
	require.Equal(t, "removed <workspace>/cache\n", got.Stderr)
}

func TestNormalizeFileResults(t *testing.T) {
	got := normalizeFileResults(map[string]string{
		"out.txt": "repo /repo/root workspace /tmp/work\n",
	}, "/repo/root", "/tmp/work")

	require.Equal(t, "repo <repo> workspace <workspace>\n", got["out.txt"])
}
