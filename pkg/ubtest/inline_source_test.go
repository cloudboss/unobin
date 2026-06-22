package ubtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type inlineUBFinding struct {
	path  string
	line  int
	token string
}

var inlineUBGreenlist = map[string]bool{
	"cmd/unobin/root/cli_test.go":          true,
	"pkg/check/constraints_test.go":        true,
	"pkg/check/helpers_test.go":            true,
	"pkg/check/types_test.go":              true,
	"pkg/codegen/encode_test.go":           true,
	"pkg/deps/deps_test.go":                true,
	"pkg/deps/fetch_test.go":               true,
	"pkg/deps/lockwalk_test.go":            true,
	"pkg/deps/scan_test.go":                true,
	"pkg/lang/constraints_test.go":         true,
	"pkg/lang/syntax/lower_test.go":        true,
	"pkg/resolve/library_test.go":          true,
	"pkg/resolve/ubwalk_test.go":           true,
	"pkg/runner/envelope_test.go":          true,
	"pkg/runner/pin_test.go":               true,
	"pkg/runner/runner_test.go":            true,
	"pkg/runtime/apply_plan_test.go":       true,
	"pkg/runtime/dag_test.go":              true,
	"pkg/runtime/executor_test.go":         true,
	"pkg/runtime/locals_test.go":           true,
	"pkg/runtime/nodes_test.go":            true,
	"pkg/runtime/plan_data_test.go":        true,
	"pkg/runtime/plan_test.go":             true,
	"pkg/runtime/refresh_test.go":          true,
	"pkg/runtime/sensitivity_test.go":      true,
	"pkg/runtime/state_moves_plan_test.go": true,
}

var inlineUBTokens = []string{
	"actions:",
	"constraints:",
	"data:",
	"factory:",
	"imports:",
	"inputs:",
	"library:",
	"locals:",
	"manifest:",
	"outputs:",
	"resources:",
	"state-moves:",
}

func TestInlineUBScannerRejectsUngreenlistedString(t *testing.T) {
	root := t.TempDir()
	writeInlineUBScannerTestFile(t, root, "pkg/example/example_test.go")

	findings, err := findInlineUBSources(root, nil)
	require.NoError(t, err)
	require.Equal(t, []inlineUBFinding{{
		path:  "pkg/example/example_test.go",
		line:  3,
		token: "factory:",
	}}, findings)
}

func TestInlineUBScannerAcceptsGreenlistedString(t *testing.T) {
	root := t.TempDir()
	rel := "pkg/example/example_test.go"
	writeInlineUBScannerTestFile(t, root, rel)

	findings, err := findInlineUBSources(root, map[string]bool{rel: true})
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestInlineUBScannerIgnoresTokenSubstring(t *testing.T) {
	_, ok := inlineUBToken("manifest:")
	require.False(t, ok)
}

func TestInlineUBGreenlistStillNeeded(t *testing.T) {
	root := repoRoot(t)
	var stale []string
	for rel := range inlineUBGreenlist {
		path := filepath.Join(root, filepath.FromSlash(rel))
		findings, err := inlineUBSourcesInFile(path, rel)
		require.NoError(t, err)
		if len(findings) == 0 {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	require.Empty(t, stale, "stale inline UB greenlist entries")
}

func TestTestFilesAvoidInlineUBSources(t *testing.T) {
	findings, err := findInlineUBSources(repoRoot(t), inlineUBGreenlist)
	require.NoError(t, err)
	if len(findings) == 0 {
		return
	}

	lines := make([]string, 0, len(findings))
	for _, finding := range findings {
		lines = append(lines, fmt.Sprintf("%s:%d: %s", finding.path, finding.line, finding.token))
	}
	t.Fatalf("inline .ub source strings need fixtures or a greenlist entry:\n%s",
		strings.Join(lines, "\n"))
}

func writeInlineUBScannerTestFile(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := "package example\n\nconst src = `" + "factory:" + " {}`\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func findInlineUBSources(root string, greenlist map[string]bool) ([]inlineUBFinding, error) {
	var findings []inlineUBFinding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && ignoredInlineUBScannerDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if greenlist[rel] {
			return nil
		}
		fileFindings, err := inlineUBSourcesInFile(path, rel)
		if err != nil {
			return err
		}
		findings = append(findings, fileFindings...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].path != findings[j].path {
			return findings[i].path < findings[j].path
		}
		return findings[i].line < findings[j].line
	})
	return findings, nil
}

func ignoredInlineUBScannerDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata"
}

func inlineUBSourcesInFile(path, rel string) ([]inlineUBFinding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	var findings []inlineUBFinding
	ast.Inspect(file, func(node ast.Node) bool {
		lit, ok := node.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if token, ok := inlineUBToken(value); ok {
			findings = append(findings, inlineUBFinding{
				path:  rel,
				line:  fset.Position(lit.Pos()).Line,
				token: token,
			})
		}
		return true
	})
	return findings, nil
}

func inlineUBToken(value string) (string, bool) {
	if !strings.ContainsAny(value, "{[\n") {
		return "", false
	}
	for line := range strings.SplitSeq(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, token := range inlineUBTokens {
			if strings.HasPrefix(line, token) {
				return token, true
			}
		}
	}
	return "", false
}
