package tree_sitter_unobin_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadmeDocumentsGrammarMaintenance(t *testing.T) {
	body, err := os.ReadFile("README.md")
	require.NoError(t, err)
	text := string(body)

	require.Contains(t, text, "npm run generate")
	require.Contains(t, text, "npm run test")
	require.Contains(t, text, "npm run compile")
	require.Contains(t, text, "queries/highlights.scm")
	require.Contains(t, text, "queries/folds.scm")
	require.Contains(t, text, "generated files")
}
