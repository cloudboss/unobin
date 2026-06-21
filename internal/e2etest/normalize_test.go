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

func TestNormalizeStateRevisionLines(t *testing.T) {
	got := normalizeDynamicText("  2026-06-20T12:00:01Z\n* 2026-06-20T12:00:02.3Z_1\n", "")

	require.Equal(t, "  <revision>\n* <revision>\n", got)
}

func TestNormalizeLocalStoreOpenRevision(t *testing.T) {
	got := normalizeDynamicText(
		"local store: open 2026-06-20T12:00:01.23Z: open: decrypt failed\n",
		"",
	)

	require.Equal(t, "local store: open <revision>: open: decrypt failed\n", got)
}
