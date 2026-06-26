package resolve

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseUBLibrarySource(t *testing.T) {
	lib, err := ParseUBLibrarySource(newUBFixtureSource(t, "parse-library/valid/simple"))

	require.NoError(t, err)
	require.Contains(t, lib.SyntaxBodies["data-source"], "thing")
}
