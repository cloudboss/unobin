package tree_sitter_unobin_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryFilesExist(t *testing.T) {
	for _, path := range []string{
		"queries/highlights.scm",
		"queries/locals.scm",
		"queries/tags.scm",
		"queries/folds.scm",
	} {
		body := readQueryFile(t, path)
		require.NotEmpty(t, body)
	}
}

func TestPackageScriptsPinTreeSitterCLI(t *testing.T) {
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	body, err := os.ReadFile("package.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &pkg))

	for _, name := range []string{"generate", "compile", "test"} {
		script := pkg.Scripts[name]
		require.Contains(t, script, "tree-sitter-cli@0.26.9")
		require.Contains(t, script, "tree-sitter")
	}
	require.Contains(t, pkg.Scripts["compile"], "src/libtree-sitter-unobin.so")
}

func TestQueriesValidateWithTreeSitter(t *testing.T) {
	treeSitter, err := exec.LookPath("tree-sitter")
	if err != nil {
		t.Skip("tree-sitter not found")
	}
	sample := "../pkg/lsp/testdata/ub/completion/valid/factory.ub"
	require.FileExists(t, sample)

	for _, path := range []string{
		"queries/highlights.scm",
		"queries/locals.scm",
		"queries/tags.scm",
		"queries/folds.scm",
	} {
		cmd := exec.Command(treeSitter,
			"query", "--grammar-path", ".", "--quiet", path, sample)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
}

func TestTagsQueryCapturesDeclarationNames(t *testing.T) {
	npm, err := exec.LookPath("npm")
	if err != nil {
		t.Skip("npm not found")
	}
	sample := "../pkg/lsp/testdata/ub/symbols/valid/factory.ub"
	require.FileExists(t, sample)

	cmd := exec.Command(npm,
		"exec", "--package=tree-sitter-cli@0.26.9", "--",
		"tree-sitter", "query", "--grammar-path", ".", "queries/tags.scm", sample)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	names := tagCaptureNames(string(out))

	for _, name := range []string{"region", "full-name", "server", "lookup", "deploy", "endpoint"} {
		require.Contains(t, names, name)
	}
	for _, name := range []string{"ami-id", "name", "value", "resources", "outputs"} {
		require.NotContains(t, names, name)
	}
}

func tagCaptureNames(output string) map[string]struct{} {
	re := regexp.MustCompile("capture: \\d+ - name, .* text: `([^`]*)`")
	names := map[string]struct{}{}
	for _, match := range re.FindAllStringSubmatch(output, -1) {
		names[match[1]] = struct{}{}
	}
	return names
}

func readQueryFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}
