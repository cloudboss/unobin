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

func TestExtensionWatchesFilesThatAffectLSPCaches(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("src", "extension.ts"))
	require.NoError(t, err)
	source := string(body)

	for _, pattern := range []string{
		"**/*.ub",
		"**/*.go",
		"**/go.mod",
		"**/project.ub",
		"**/project-lock.ub",
	} {
		require.Contains(t, source, "createFileSystemWatcher('"+pattern+"')")
	}
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
		ScopeName  string                 `json:"scopeName"`
		Patterns   []any                  `json:"patterns"`
		Repository map[string]textMateSet `json:"repository"`
	}
	readJSON(t, filepath.Join("syntaxes", "unobin.tmLanguage.json"), &grammar)

	require.Equal(t, "source.unobin", grammar.ScopeName)
	require.NotEmpty(t, grammar.Patterns)
	for _, name := range []string{
		"comments",
		"strings",
		"escapes",
		"interpolations",
		"declarations",
		"properties",
		"types",
		"constants",
		"selectors",
		"functions",
		"paths",
		"operators",
		"punctuation",
	} {
		require.Contains(t, grammar.Repository, name)
		require.NotEmpty(t, grammar.Repository[name].Patterns)
	}
}

func TestTextMateGrammarScopesMatchEditorScheme(t *testing.T) {
	var grammar struct {
		Repository map[string]textMateSet `json:"repository"`
	}
	readJSON(t, filepath.Join("syntaxes", "unobin.tmLanguage.json"), &grammar)
	scopes := textMateScopes(grammar.Repository)

	for _, scope := range []string{
		"comment.line.number-sign.unobin",
		"string.quoted.single.unobin",
		"string.quoted.single.interpolated.unobin",
		"string.quoted.triple.unobin",
		"string.quoted.triple.interpolated.unobin",
		"constant.character.escape.unobin",
		"invalid.illegal.escape.unobin",
		"meta.interpolation.unobin",
		"keyword.declaration.unobin",
		"keyword.control.unobin",
		"keyword.other.directive.unobin",
		"variable.other.property.unobin",
		"storage.type.unobin",
		"support.type.unobin",
		"constant.language.unobin",
		"constant.numeric.unobin",
		"entity.name.function.selector.unobin",
		"entity.name.function.call.unobin",
		"variable.language.unobin",
		"variable.other.readwrite.unobin",
		"keyword.operator.unobin",
		"punctuation.accessor.dot.unobin",
		"punctuation.accessor.guarded.unobin",
		"punctuation.separator.key-value.unobin",
		"punctuation.section.block.begin.unobin",
	} {
		require.Contains(t, scopes, scope)
	}
}

type textMateSet struct {
	Patterns []textMatePattern `json:"patterns"`
}

type textMatePattern struct {
	Name     string            `json:"name"`
	Patterns []textMatePattern `json:"patterns"`
}

func textMateScopes(repository map[string]textMateSet) map[string]bool {
	scopes := map[string]bool{}
	var visit func(patterns []textMatePattern)
	visit = func(patterns []textMatePattern) {
		for _, pattern := range patterns {
			if pattern.Name != "" {
				scopes[pattern.Name] = true
			}
			visit(pattern.Patterns)
		}
	}
	for _, set := range repository {
		visit(set.Patterns)
	}
	return scopes
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, target))
}
