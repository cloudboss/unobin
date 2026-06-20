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
