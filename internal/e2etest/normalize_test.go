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

func TestNormalizeStateRevOutput(t *testing.T) {
	got := normalizeDynamicText("State rev: 2026-06-20T12:00:01.23Z\n", "")

	require.Equal(t, "State rev: <revision>\n", got)
}

func TestNormalizeRunViewURL(t *testing.T) {
	got := normalizeDynamicText(
		"Run view: http://127.0.0.1:12345/0123456789abcdef0123456789abcdef/\n",
		"",
	)

	require.Equal(t, "Run view: <run-view>\n", got)
}

func TestNormalizeUIEventTime(t *testing.T) {
	got := normalizeDynamicText("[23:15:31] ran action.hi (12ms)\n", "")

	require.Equal(t, "[<time>] ran action.hi (<elapsed>)\n", got)
}
