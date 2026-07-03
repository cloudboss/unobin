package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootIncludesModZipCommand(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"modzip"})
	require.NoError(t, err)
	require.Equal(t, "modzip", cmd.Name())

	alias, _, err := rootCmd.Find([]string{"mkmodzip"})
	require.NoError(t, err)
	require.Equal(t, "modzip", alias.Name())
}
