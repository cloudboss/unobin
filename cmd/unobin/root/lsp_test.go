package root

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLSPCommandExists(t *testing.T) {
	out, err := runCommand(t, "lsp", "--help")
	require.NoError(t, err)
	require.Contains(t, out, "lsp")
	require.Contains(t, out, "--trace")
	require.Contains(t, out, "--log")
}
