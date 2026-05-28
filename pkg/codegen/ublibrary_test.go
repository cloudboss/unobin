package codegen

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func parseUB(t *testing.T, name, src string) *lang.File {
	t.Helper()
	f, err := lang.ParseSource(name, []byte(src))
	require.NoError(t, err)
	return f
}

func TestGenerateUBLibraryProducesValidGo(t *testing.T) {
	manifest := parseUB(t, "library.ub", `description: 'test library'

exports: {
  cluster: 'cluster.ub'
}
`)
	body := parseUB(t, "cluster.ub", `description: 'a cluster'

resources: {
  local: {
    file: {
      x: { path: '/tmp/x', content: 'hi', mode: 420 }
    }
  }
}
`)

	out, err := GenerateUBLibrary("net", manifest, map[string]*lang.File{"cluster": body}, nil)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "net.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)
}

func TestGenerateUBLibraryHasExpectedShape(t *testing.T) {
	manifest := parseUB(t, "library.ub", `description: 'test library'

exports: {
  alpha: 'alpha.ub'
  beta:  'beta.ub'
}
`)
	bodies := map[string]*lang.File{
		"alpha": parseUB(t, "alpha.ub", "description: 'a'"),
		"beta":  parseUB(t, "beta.ub", "description: 'b'"),
	}

	out, err := GenerateUBLibrary("net", manifest, bodies, nil)
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, "package net")
	require.Contains(t, s, "func Library() *runtime.Library")
	require.Regexp(t, `Name:\s*"net"`, s)
	require.Regexp(t, `Description:\s*"test library"`, s)
	require.Regexp(t, `Composites:\s*map\[string\]\*runtime\.CompositeType\{`, s)
	require.Regexp(t, `"alpha":\s*\{\s*Name:\s*"alpha"`, s)
	require.Regexp(t, `"beta":\s*\{\s*Name:\s*"beta"`, s)

	alphaAt := strings.Index(s, `"alpha":`)
	betaAt := strings.Index(s, `"beta":`)
	require.True(t, alphaAt > 0 && betaAt > 0)
	require.Less(t, alphaAt, betaAt, "composites should be in sorted order")
}

func TestGenerateUBLibraryEmitsPerCompositeLibraries(t *testing.T) {
	manifest := parseUB(t, "library.ub", `description: 'wraps an inner UB library'

exports: {
  greeting: 'greeting.ub'
}
`)
	body := parseUB(t, "greeting.ub", `description: 'a greeting'

resources: {
  helloer: { hello: { file: { message: var.message, path: var.path } } }
}
`)
	imports := map[string]map[string]string{
		"greeting": {
			"helloer": "github.com/cloudboss/unobin-libraries-scratch/ub/helloer",
			"local":   "github.com/cloudboss/unobin/pkg/libraries/local",
		},
	}

	out, err := GenerateUBLibrary("greeter", manifest, map[string]*lang.File{"greeting": body}, imports)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "greeter.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)

	s := string(out)
	require.Contains(t, s, `lib_helloer "github.com/cloudboss/unobin-libraries-scratch/ub/helloer"`,
		"each unique composite-imported path gets its own Go-level alias")
	require.Contains(t, s, `lib_local "github.com/cloudboss/unobin/pkg/libraries/local"`)
	require.Regexp(t, `Libraries:\s*map\[string\]\*runtime\.Library\{`,
		s, "the composite carries its resolved imports")
	require.Contains(t, s, `"helloer": lib_helloer.Library()`)
	require.Contains(t, s, `"local":   lib_local.Library()`)
}

func TestGenerateUBLibrarySharesIdentForSamePath(t *testing.T) {
	manifest := parseUB(t, "library.ub", `description: 'two composites share an import'

exports: {
  alpha: 'alpha.ub'
  beta:  'beta.ub'
}
`)
	bodies := map[string]*lang.File{
		"alpha": parseUB(t, "alpha.ub", `description: 'a'`),
		"beta":  parseUB(t, "beta.ub", `description: 'b'`),
	}
	// Both composites import the same package; the generated file
	// should declare it once and bind both composite-local aliases to
	// the same Go-level identifier.
	imports := map[string]map[string]string{
		"alpha": {"local": "github.com/cloudboss/unobin/pkg/libraries/local"},
		"beta":  {"thing": "github.com/cloudboss/unobin/pkg/libraries/local"},
	}
	out, err := GenerateUBLibrary("test", manifest, bodies, imports)
	require.NoError(t, err)

	s := string(out)
	require.Equal(t, 1,
		strings.Count(s, `"github.com/cloudboss/unobin/pkg/libraries/local"`),
		"the same path should be imported only once across composites")
	require.Contains(t, s, `"local": lib_local.Library()`)
	require.Contains(t, s, `"thing": lib_local.Library()`)
}

func TestGenerateUBLibraryErrorsOnMissingBody(t *testing.T) {
	manifest := parseUB(t, "library.ub", `exports: { cluster: 'cluster.ub' }`)
	_, err := GenerateUBLibrary("net", manifest, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cluster")
	require.Contains(t, err.Error(), "no body")
}

func TestGenerateUBLibraryEmptyExports(t *testing.T) {
	manifest := parseUB(t, "library.ub", `description: 'no exports'`)
	out, err := GenerateUBLibrary("empty", manifest, nil, nil)
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, "package empty")
	require.Regexp(t, `Description:\s*"no exports"`, s)
	require.Regexp(t, `Composites:\s*map\[string\]\*runtime\.CompositeType\{`, s)
}

func TestGenerateUBLibraryRejectsEmptyAlias(t *testing.T) {
	manifest := parseUB(t, "library.ub", `description: 'x'`)
	_, err := GenerateUBLibrary("", manifest, nil, nil)
	require.Error(t, err)
}

// TestGenerateUBLibraryCompilesWithCaller writes the generated Go
// source into a temporary Go library, alongside a small main that
// imports the package and prints attributes from Library() that the
// test then asserts on. The temporary library has a replace directive
// pointing back at the unobin checkout, so `go build` can resolve
// the runtime and lang imports without going to the network.
func TestGenerateUBLibraryCompilesWithCaller(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped: spawns `go run` and is slow")
	}

	manifest := parseUB(t, "library.ub", `description: 'test library'

exports: {
  cluster: 'cluster.ub'
}
`)
	body := parseUB(t, "cluster.ub", `description: 'a cluster'

resources: {
  local: {
    file: {
      x: { path: '/tmp/x', content: 'hi', mode: 420 }
    }
  }
}
`)

	out, err := GenerateUBLibrary("net", manifest, map[string]*lang.File{"cluster": body}, nil)
	require.NoError(t, err)

	rootDir := findUnobinRoot(t)
	tmp := t.TempDir()

	pkgDir := filepath.Join(tmp, "internal", "net")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "net.go"), out, 0o644))

	main := `package main

import (
	"fmt"
	"os"

	"example.test/check/internal/net"
)

func main() {
	lib := net.Library()
	fmt.Printf("name=%s\n", lib.Name)
	fmt.Printf("description=%s\n", lib.Description)
	fmt.Printf("composites=%d\n", len(lib.Composites))
	for name, ct := range lib.Composites {
		fmt.Printf("composite=%s body-fields=%d\n", name, len(ct.Body.Body.Fields))
	}
	if lib.Composites["cluster"] == nil {
		fmt.Fprintln(os.Stderr, "missing cluster composite")
		os.Exit(1)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "main.go"), []byte(main), 0o644))

	goMod := fmt.Sprintf(`module example.test/check

go 1.26

require github.com/cloudboss/unobin v0.0.0

replace github.com/cloudboss/unobin => %s
`, rootDir)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goMod), 0o644))

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = tmp
	if tidyOut, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy:\n%s", tidyOut)
	}

	run := exec.Command("go", "run", ".")
	run.Dir = tmp
	runOut, err := run.CombinedOutput()
	require.NoError(t, err, "go run:\n%s", runOut)

	got := string(runOut)
	require.Contains(t, got, "name=net")
	require.Contains(t, got, "description=test library")
	require.Contains(t, got, "composites=1")
	require.Contains(t, got, "composite=cluster body-fields=2")
}

func findUnobinRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	for d := cwd; ; d = filepath.Dir(d) {
		body, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil && strings.Contains(string(body), "module github.com/cloudboss/unobin") {
			return d
		}
		if d == filepath.Dir(d) {
			break
		}
	}
	t.Fatal("could not find unobin root")
	return ""
}
