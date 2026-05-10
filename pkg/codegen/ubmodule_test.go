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

func TestGenerateUBModuleProducesValidGo(t *testing.T) {
	manifest := parseUB(t, "module.ub", `description: 'test module'

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

	out, err := GenerateUBModule("net", manifest, map[string]*lang.File{"cluster": body}, nil)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "net.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)
}

func TestGenerateUBModuleHasExpectedShape(t *testing.T) {
	manifest := parseUB(t, "module.ub", `description: 'test module'

exports: {
  alpha: 'alpha.ub'
  beta:  'beta.ub'
}
`)
	bodies := map[string]*lang.File{
		"alpha": parseUB(t, "alpha.ub", "description: 'a'"),
		"beta":  parseUB(t, "beta.ub", "description: 'b'"),
	}

	out, err := GenerateUBModule("net", manifest, bodies, nil)
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, "package net")
	require.Contains(t, s, "func Module() *runtime.Module")
	require.Regexp(t, `Name:\s*"net"`, s)
	require.Regexp(t, `Description:\s*"test module"`, s)
	require.Regexp(t, `Composites:\s*map\[string\]\*runtime\.CompositeType\{`, s)
	require.Regexp(t, `"alpha":\s*\{\s*Name:\s*"alpha"`, s)
	require.Regexp(t, `"beta":\s*\{\s*Name:\s*"beta"`, s)

	alphaAt := strings.Index(s, `"alpha":`)
	betaAt := strings.Index(s, `"beta":`)
	require.True(t, alphaAt > 0 && betaAt > 0)
	require.Less(t, alphaAt, betaAt, "composites should be in sorted order")
}

func TestGenerateUBModuleEmitsPerCompositeModules(t *testing.T) {
	manifest := parseUB(t, "module.ub", `description: 'wraps an inner UB module'

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
			"helloer": "github.com/cloudboss/unobin-modules-scratch/ub/helloer",
			"local":   "github.com/cloudboss/unobin/pkg/modules/local",
		},
	}

	out, err := GenerateUBModule("greeter", manifest, map[string]*lang.File{"greeting": body}, imports)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "greeter.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)

	s := string(out)
	require.Contains(t, s, `mod_helloer "github.com/cloudboss/unobin-modules-scratch/ub/helloer"`,
		"each unique composite-imported path gets its own Go-level alias")
	require.Contains(t, s, `mod_local "github.com/cloudboss/unobin/pkg/modules/local"`)
	require.Regexp(t, `Modules:\s*map\[string\]\*runtime\.Module\{`,
		s, "the composite carries its resolved imports")
	require.Contains(t, s, `"helloer": mod_helloer.Module()`)
	require.Contains(t, s, `"local":   mod_local.Module()`)
}

func TestGenerateUBModuleSharesIdentForSamePath(t *testing.T) {
	manifest := parseUB(t, "module.ub", `description: 'two composites share an import'

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
		"alpha": {"local": "github.com/cloudboss/unobin/pkg/modules/local"},
		"beta":  {"thing": "github.com/cloudboss/unobin/pkg/modules/local"},
	}
	out, err := GenerateUBModule("test", manifest, bodies, imports)
	require.NoError(t, err)

	s := string(out)
	require.Equal(t, 1,
		strings.Count(s, `"github.com/cloudboss/unobin/pkg/modules/local"`),
		"the same path should be imported only once across composites")
	require.Contains(t, s, `"local": mod_local.Module()`)
	require.Contains(t, s, `"thing": mod_local.Module()`)
}

func TestGenerateUBModuleErrorsOnMissingBody(t *testing.T) {
	manifest := parseUB(t, "module.ub", `exports: { cluster: 'cluster.ub' }`)
	_, err := GenerateUBModule("net", manifest, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cluster")
	require.Contains(t, err.Error(), "no body")
}

func TestGenerateUBModuleEmptyExports(t *testing.T) {
	manifest := parseUB(t, "module.ub", `description: 'no exports'`)
	out, err := GenerateUBModule("empty", manifest, nil, nil)
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, "package empty")
	require.Regexp(t, `Description:\s*"no exports"`, s)
	require.Regexp(t, `Composites:\s*map\[string\]\*runtime\.CompositeType\{`, s)
}

func TestGenerateUBModuleRejectsEmptyAlias(t *testing.T) {
	manifest := parseUB(t, "module.ub", `description: 'x'`)
	_, err := GenerateUBModule("", manifest, nil, nil)
	require.Error(t, err)
}

// TestGenerateUBModuleCompilesWithCaller writes the generated Go
// source into a temporary Go module, alongside a small main that
// imports the package and prints attributes from Module() that the
// test then asserts on. The temporary module has a replace directive
// pointing back at the unobin checkout, so `go build` can resolve
// the runtime and lang imports without going to the network.
func TestGenerateUBModuleCompilesWithCaller(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped: spawns `go run` and is slow")
	}

	manifest := parseUB(t, "module.ub", `description: 'test module'

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

	out, err := GenerateUBModule("net", manifest, map[string]*lang.File{"cluster": body}, nil)
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
	mod := net.Module()
	fmt.Printf("name=%s\n", mod.Name)
	fmt.Printf("description=%s\n", mod.Description)
	fmt.Printf("composites=%d\n", len(mod.Composites))
	for name, ct := range mod.Composites {
		fmt.Printf("composite=%s body-fields=%d\n", name, len(ct.Body.Body.Fields))
	}
	if mod.Composites["cluster"] == nil {
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
	require.Contains(t, got, "description=test module")
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
