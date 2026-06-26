package root

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckCommandDefaultPathPrintsOK(t *testing.T) {
	setCLIVersion(t, "dev")
	t.Chdir(filepath.Join("testdata", "ub", "check-command", "valid", "default-factory"))

	out, err := runCommand(t, "check")

	require.NoError(t, err)
	require.Equal(t, "OK\n", out)
}

func TestCheckCommandRejectsUnknownRole(t *testing.T) {
	setCLIVersion(t, "dev")
	path := filepath.Join(
		"testdata", "ub", "check-command", "invalid", "unknown-role", "loose.ub")

	out, err := runCommand(t, "check", "-p", path)

	require.Error(t, err)
	require.Contains(t, out, "cannot determine UB file role")
}
