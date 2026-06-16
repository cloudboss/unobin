package compile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/require"
)

// TestExamplesResidualUnknowns compiles every example with the real
// front end and records each expression position whose inferred type
// still contains Unknown. The fixture pins the remaining positions:
// an addition is a regression to explain, a removal is progress to
// record, and the fixture shrinking to empty is the goal. The compile
// itself must succeed, so this is also the test that every example
// compiles.
func TestExamplesResidualUnknowns(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	exRoot := filepath.Join(repoRoot, "examples")
	entries, err := os.ReadDir(exRoot)
	require.NoError(t, err)

	var residuals []string
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		name := ent.Name()
		stack, err := FactorySourcePath(filepath.Join(exRoot, name))
		if err != nil {
			continue
		}
		residuals = append(residuals, exampleResiduals(t, repoRoot, name, stack)...)
	}
	slices.Sort(residuals)

	want := fixtureLines(t, filepath.Join("testdata", "residual-unknowns.txt"))
	require.Equal(t, want, residuals)
}

type exprPosition struct {
	file string
	line int
	col  int
}

// exampleResiduals compiles one example and returns the positions
// whose every observation contains Unknown; one typed observation
// clears a position, since the checker then knows its type somewhere.
func exampleResiduals(t *testing.T, repoRoot, name, stack string) []string {
	t.Helper()
	unknownAt := map[exprPosition]string{}
	clearedAt := map[exprPosition]bool{}
	observe := func(e lang.Expr, ty typecheck.Type) {
		// An empty list literal has no element type by nature; its
		// list(unknown) joins away in context and is not a checker gap.
		if a, ok := e.(*lang.ArrayLit); ok && len(a.Elements) == 0 {
			return
		}
		pos := e.Span().Start
		key := exprPosition{pos.File, pos.Line, pos.Column}
		if !ty.ContainsUnknown() {
			clearedAt[key] = true
			return
		}
		if _, done := unknownAt[key]; !done {
			unknownAt[key] = ty.String()
		}
	}
	err := Run(Options{
		FactoryPath:   stack,
		OutDir:        t.TempDir(),
		StackName:     name,
		GoVersion:     GoMajorMinor(),
		CLIVersion:    "dev",
		ReplaceUnobin: repoRoot,
		Stdout:        io.Discard,
		Stderr:        io.Discard,
		TypeObserver:  observe,
	})
	require.NoError(t, err, "example %s must compile", name)

	var out []string
	for key, tyStr := range unknownAt {
		if clearedAt[key] {
			continue
		}
		out = append(out, fmt.Sprintf("%s:%d:%d %s",
			stablePath(repoRoot, key.file), key.line, key.col, tyStr))
	}
	return out
}

// stablePath renders a span's file machine-independently: repo files
// relative to the repo root, dependency-cache files relative to the
// cache root (the module@version segment keeps them pinned by the
// lock), anything else by name alone.
func stablePath(repoRoot, file string) string {
	if rel, err := filepath.Rel(repoRoot, file); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	if cache, err := os.UserCacheDir(); err == nil {
		root := filepath.Join(cache, "unobin")
		if rel, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(file)
}

func fixtureLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var out []string
	for ln := range strings.SplitSeq(strings.TrimRight(string(b), "\n"), "\n") {
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		out = append(out, ln)
	}
	return out
}
