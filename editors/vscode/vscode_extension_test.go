package vscode_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPackageDeclaresUnobinLanguage(t *testing.T) {
	var pkg struct {
		Main        string   `json:"main"`
		Activation  []string `json:"activationEvents"`
		Contributes struct {
			Languages []struct {
				ID         string   `json:"id"`
				Extensions []string `json:"extensions"`
			} `json:"languages"`
			Configuration struct {
				Properties map[string]any `json:"properties"`
			} `json:"configuration"`
		} `json:"contributes"`
	}
	readJSON(t, "package.json", &pkg)

	require.Equal(t, "./out/extension.js", pkg.Main)
	require.Contains(t, pkg.Activation, "onLanguage:unobin")
	require.Len(t, pkg.Contributes.Languages, 1)
	require.Equal(t, "unobin", pkg.Contributes.Languages[0].ID)
	require.Contains(t, pkg.Contributes.Languages[0].Extensions, ".ub")
	require.Contains(t, pkg.Contributes.Configuration.Properties, "unobin.path")
}

func TestExtensionStartsUnobinLSP(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("src", "extension.ts"))
	require.NoError(t, err)
	source := string(body)

	require.Contains(t, source, "buildServerOptions")
	require.Contains(t, source, "args: ['lsp']")
	require.Contains(t, source, "get<string>('path', 'unobin')")
}

func TestTextMateGrammarIsJSON(t *testing.T) {
	var grammar struct {
		ScopeName string `json:"scopeName"`
		Patterns  []any  `json:"patterns"`
	}
	readJSON(t, filepath.Join("syntaxes", "unobin.tmLanguage.json"), &grammar)

	require.Equal(t, "source.unobin", grammar.ScopeName)
	require.NotEmpty(t, grammar.Patterns)
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, target))
}
