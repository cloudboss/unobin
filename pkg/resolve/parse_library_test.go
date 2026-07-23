package resolve

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseUBLibrarySource(t *testing.T) {
	lib, err := ParseUBLibrarySource(newUBFixtureSource(t, "parse-library/valid/simple"))

	require.NoError(t, err)
	require.Contains(t, lib.SyntaxBodies["data-source"], "thing")
	entries := lib.CompositeEntries()
	require.Len(t, entries, 1)
	require.Equal(t, "library.ub", entries[0].SourceFile.PackageRelPath)
}
