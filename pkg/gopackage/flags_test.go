package gopackage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContextFlags(t *testing.T) {
	context := Context{
		GOFLAGS: `-tags=custom '-overlay=/a path/overlay.json' "-modfile=/b path/go.mod"`,
	}
	flags, err := context.Flags()
	require.NoError(t, err)
	require.Equal(t, []string{
		"-tags=custom", "-overlay=/a path/overlay.json", "-modfile=/b path/go.mod",
	}, flags)
	context.GOFLAGS = "'-overlay=unfinished"
	_, err = context.Flags()
	require.Error(t, err)
}
