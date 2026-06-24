package docs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadmeDocumentsEditorSupport(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "README.md"))
	require.NoError(t, err)
	text := string(body)

	require.Contains(t, text, "## Editor support")
	require.Contains(t, text, "unobin lsp")
	require.Contains(t, text, "--trace")
	require.Contains(t, text, "Emacs")
	require.Contains(t, text, "VS Code")
	require.Contains(t, text, "Tree-sitter")
	require.Contains(t, text, "does not fetch dependencies")
}
