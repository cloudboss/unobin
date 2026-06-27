package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoGeneratedFactorySourceStringPath(t *testing.T) {
	root := findUnobinRoot(t)
	patterns := []sourceStringPattern{
		{
			name: "runner info factory body string field",
			re:   regexp.MustCompile(`FactoryBody\s+string`),
		},
		{
			name: "generated quoted factory body",
			re:   regexp.MustCompile(`factoryBody\s*=\s*"`),
		},
		{
			name: "runner reparses info factory body",
			re: regexp.MustCompile(
				`syntax\.ParseSource\("factory\.ub", \[\]byte\(info\.FactoryBody\)\)`),
		},
	}

	matches := scanSourceStringPathMatches(t, root, patterns)
	require.Empty(t, matches, "old generated-factory source-string paths remain")
}

type sourceStringPattern struct {
	name string
	re   *regexp.Regexp
}

func scanSourceStringPathMatches(
	t *testing.T,
	root string,
	patterns []sourceStringPattern,
) []string {
	t.Helper()
	var matches []string
	for _, dir := range []string{"pkg", "cmd", "internal", "tests/e2e/testdata/source-cases"} {
		base := filepath.Join(root, filepath.FromSlash(dir))
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			matches = append(matches, sourceStringPathMatches(
				filepath.ToSlash(rel), string(body), patterns)...)
			return nil
		})
		require.NoError(t, err)
	}
	return matches
}

func sourceStringPathMatches(
	path string,
	body string,
	patterns []sourceStringPattern,
) []string {
	var matches []string
	for lineNo, line := range strings.Split(body, "\n") {
		for _, pattern := range patterns {
			if pattern.re.MatchString(line) {
				matches = append(matches,
					fmt.Sprintf("%s:%d: %s: %s", path, lineNo+1, pattern.name, line))
			}
		}
	}
	return matches
}
