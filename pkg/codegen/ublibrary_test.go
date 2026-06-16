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
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/stretchr/testify/require"
)

func parseUB(t *testing.T, name, src string) *lang.File {
	t.Helper()
	f, err := lang.ParseSource(name, []byte(src))
	require.NoError(t, err)
	return f
}

func resourceBodies(bodies map[string]*lang.File) map[string]map[string]*lang.File {
	return map[string]map[string]*lang.File{"resource": bodies}
}

func parseSyntaxUB(t *testing.T, name, src string) syntax.FactoryBody {
	t.Helper()
	f, err := syntax.ParseSource(name, []byte(src))
	require.NoError(t, err)
	require.NotNil(t, f.Library)
	require.Len(t, f.Library.Exports, 1)
	return f.Library.Exports[0].Body
}

func resourceSyntaxBodies(
	bodies map[string]syntax.FactoryBody,
) map[string]map[string]syntax.FactoryBody {
	return map[string]map[string]syntax.FactoryBody{"resource": bodies}
}

func compositeImports(
	kind string,
	imports map[string]map[string]string,
) map[string]map[string]map[string]string {
	return map[string]map[string]map[string]string{kind: imports}
}

func TestGenerateUBLibraryProducesValidGo(t *testing.T) {
	body := parseUB(t, "library.ub", `description: 'a cluster'

resources: { local.file.x: { path: '/tmp/x', content: 'hi', mode: 420 } }
`)
	bodies := map[string]*lang.File{"cluster": body}

	out, err := GenerateUBLibrary("net", resourceBodies(bodies), nil, nil, nil)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "net.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)
}

func TestGenerateUBLibraryHasExpectedShape(t *testing.T) {
	bodies := map[string]*lang.File{
		"alpha": parseUB(t, "library.ub", "description: 'a'"),
		"beta":  parseUB(t, "library.ub", "description: 'b'"),
	}

	out, err := GenerateUBLibrary("net", resourceBodies(bodies), nil, nil, nil)
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, "package net")
	require.Contains(t, s, "func Library() *runtime.Library")
	require.Regexp(t, `Name:\s*"net"`, s)
	require.Regexp(t, `ResourceComposites:\s*map\[string\]\*runtime\.CompositeType\{`, s)
	require.Regexp(t, `"alpha":\s*\{\s*Name:\s*"alpha",\s*Kind:\s*runtime\.NodeResource`, s)
	require.Regexp(t, `"beta":\s*\{\s*Name:\s*"beta",\s*Kind:\s*runtime\.NodeResource`, s)

	alphaAt := strings.Index(s, `"alpha":`)
	betaAt := strings.Index(s, `"beta":`)
	require.True(t, alphaAt > 0 && betaAt > 0)
	require.Less(t, alphaAt, betaAt, "composites should be in sorted order")
}

func TestGenerateUBLibraryEmitsSyntaxBody(t *testing.T) {
	body := parseUB(t, "library.ub", `description: 'generic'`)
	syntaxBody := parseSyntaxUB(t, "library.ub", `
greeting: resource {
  locals: { target: resource.helper.path }
  resources: {
    helper: local.fs-file { path: '/tmp/helper' }
    file: local.fs-file { path: local.target }
  }
}
`)

	out, err := GenerateUBLibrary(
		"net",
		resourceBodies(map[string]*lang.File{"greeting": body}),
		resourceSyntaxBodies(map[string]syntax.FactoryBody{"greeting": syntaxBody}),
		nil,
		nil,
	)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "net.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)

	s := string(out)
	require.Contains(t, s, `"github.com/cloudboss/unobin/pkg/lang/syntax"`)
	require.Contains(t, s, `SyntaxBody: &syntax.FactoryBody{`)
	require.Contains(t, s, `Locals: []syntax.LocalDecl{`)
	require.Contains(t, s, `Resources: []syntax.NodeDecl{`)
}

func TestGenerateUBLibrarySplitsByKind(t *testing.T) {
	bodies := map[string]map[string]*lang.File{
		"resource": {"box": parseUB(t, "library.ub", "description: 'r'")},
		"data":     {"lookup": parseUB(t, "library.ub", "description: 'd'")},
		"action":   {"run": parseUB(t, "library.ub", "description: 'a'")},
	}

	out, err := GenerateUBLibrary("mixed", bodies, nil, nil, nil)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "mixed.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)

	s := string(out)
	require.Regexp(t, `ResourceComposites:\s*map\[string\]\*runtime\.CompositeType\{`, s)
	require.Regexp(t, `DataComposites:\s*map\[string\]\*runtime\.CompositeType\{`, s)
	require.Regexp(t, `ActionComposites:\s*map\[string\]\*runtime\.CompositeType\{`, s)
	require.Regexp(t, `"box":\s*\{\s*Name:\s*"box",\s*Kind:\s*runtime\.NodeResource`, s)
	require.Regexp(t, `"lookup":\s*\{\s*Name:\s*"lookup",\s*Kind:\s*runtime\.NodeData`, s)
	require.Regexp(t, `"run":\s*\{\s*Name:\s*"run",\s*Kind:\s*runtime\.NodeAction`, s)
}

func TestGenerateUBLibraryRejectsUnknownKind(t *testing.T) {
	bodies := map[string]*lang.File{"x": parseUB(t, "library.ub", "description: 'x'")}
	_, err := GenerateUBLibrary("net", map[string]map[string]*lang.File{"widget": bodies}, nil, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown kind")
}

func TestGenerateUBLibraryAllowsSameNameAcrossKinds(t *testing.T) {
	bodies := map[string]map[string]*lang.File{
		"resource": {"vpc": parseUB(t, "library.ub", "description: 'r'")},
		"data":     {"vpc": parseUB(t, "library.ub", "description: 'd'")},
		"action":   {"vpc": parseUB(t, "library.ub", "description: 'a'")},
	}

	out, err := GenerateUBLibrary("net", bodies, nil, nil, nil)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "net.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)

	s := string(out)
	require.Equal(t, 3, strings.Count(s, `"vpc":`))
	require.Regexp(t, `"vpc":\s*\{\s*Name:\s*"vpc",\s*Kind:\s*runtime\.NodeResource`, s)
	require.Regexp(t, `"vpc":\s*\{\s*Name:\s*"vpc",\s*Kind:\s*runtime\.NodeData`, s)
	require.Regexp(t, `"vpc":\s*\{\s*Name:\s*"vpc",\s*Kind:\s*runtime\.NodeAction`, s)
}

func TestGenerateUBLibraryEmitsPerCompositeLibraries(t *testing.T) {
	body := parseUB(t, "library.ub", `description: 'a greeting'

resources: { helloer.hello.file: { message: var.message, path: var.path } }
`)
	bodies := map[string]*lang.File{"greeting": body}
	imports := map[string]map[string]string{
		"greeting": {
			"helloer": "github.com/example/helloer",
			"local":   "github.com/cloudboss/unobin/pkg/libraries/local",
		},
	}

	out, err := GenerateUBLibrary(
		"greeter", resourceBodies(bodies), nil, compositeImports("resource", imports), nil)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "greeter.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)

	s := string(out)
	require.Contains(t, s, `lib_helloer "github.com/example/helloer"`,
		"each unique composite-imported path gets its own Go-level alias")
	require.Contains(t, s, `lib_local "github.com/cloudboss/unobin/pkg/libraries/local"`)
	require.Regexp(t, `Libraries:\s*map\[string\]\*runtime\.Library\{`,
		s, "the composite carries its resolved imports")
	require.Contains(t, s, `"helloer": lib_helloer.Library()`)
	require.Contains(t, s, `"local":   lib_local.Library()`)
}

func TestGenerateUBLibrarySharesIdentForSamePath(t *testing.T) {
	bodies := map[string]*lang.File{
		"alpha": parseUB(t, "library.ub", `description: 'a'`),
		"beta":  parseUB(t, "library.ub", `description: 'b'`),
	}
	// Both composites import the same package; the generated file
	// should declare it once and bind both composite-local aliases to
	// the same Go-level identifier.
	imports := map[string]map[string]string{
		"alpha": {"local": "github.com/cloudboss/unobin/pkg/libraries/local"},
		"beta":  {"thing": "github.com/cloudboss/unobin/pkg/libraries/local"},
	}
	out, err := GenerateUBLibrary(
		"test", resourceBodies(bodies), nil, compositeImports("resource", imports), nil)
	require.NoError(t, err)

	s := string(out)
	require.Equal(t, 1,
		strings.Count(s, `"github.com/cloudboss/unobin/pkg/libraries/local"`),
		"the same path should be imported only once across composites")
	require.Contains(t, s, `"local": lib_local.Library()`)
	require.Contains(t, s, `"thing": lib_local.Library()`)
}

// TestGenerateUBLibraryEmbedsGoLibrarySpecs locks the generated form
// for a composite-imported Go library that declares constraints and
// defaults: the library is constructed once, its specs are attached,
// and the binding shares the instance, while a spec-free import keeps
// the plain inline call.
func TestGenerateUBLibraryEmbedsGoLibrarySpecs(t *testing.T) {
	bodies := map[string]*lang.File{
		"archive": parseUB(t, "library.ub", "description: 'a'"),
	}
	imports := map[string]map[string]string{
		"archive": {
			"disk":  "github.com/example/disk",
			"plain": "github.com/example/plain",
		},
	}
	goSpecs := map[string]GoLibrarySpecs{
		"github.com/example/disk": {
			Constraints: map[string][]lang.ConstraintSpec{
				"resource.file": {
					{Kind: "predicate", Require: "var.path != null",
						Message: "a file needs a path"},
				},
			},
			Defaults: map[string][]lang.DefaultSpec{
				"resource.file": {
					{Field: "var.mode", Value: "420"},
					{Field: "var.create-directory", Optional: true},
				},
			},
		},
	}

	out, err := GenerateUBLibrary(
		"files", resourceBodies(bodies), nil, compositeImports("resource", imports), goSpecs)
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, `diskLib := lib_disk.Library()`)
	require.Contains(t, s, `diskLib.Constraints = map[string][]lang.ConstraintSpec{`)
	require.Contains(t, s, `{Kind: "predicate", Require: "var.path != null"`)
	require.Contains(t, s, `{Field: "var.mode", Value: "420"}`)
	require.Contains(t, s, `{Field: "var.create-directory", Optional: true}`)
	require.Contains(t, s, `Name: "archive"`)
	require.Contains(t, s, `Kind: runtime.NodeResource`)
	require.Contains(t, s, `Path: "library.ub"`)
	require.Contains(t, s, `"disk":  diskLib`)
	require.Contains(t, s, `"plain": lib_plain.Library()`)
}

// TestGenerateUBLibrarySharesSpecsAcrossComposites proves two
// composites importing the same spec-bearing path bind one shared
// instance, so the specs are emitted once.
func TestGenerateUBLibrarySharesSpecsAcrossComposites(t *testing.T) {
	bodies := map[string]*lang.File{
		"alpha": parseUB(t, "library.ub", "description: 'a'"),
		"beta":  parseUB(t, "library.ub", "description: 'b'"),
	}
	imports := map[string]map[string]string{
		"alpha": {"disk": "github.com/example/disk"},
		"beta":  {"d": "github.com/example/disk"},
	}
	goSpecs := map[string]GoLibrarySpecs{
		"github.com/example/disk": {
			Defaults: map[string][]lang.DefaultSpec{
				"resource.file": {{Field: "var.mode", Value: "420"}},
			},
		},
	}

	out, err := GenerateUBLibrary(
		"files", resourceBodies(bodies), nil, compositeImports("resource", imports), goSpecs)
	require.NoError(t, err)

	s := string(out)
	require.Equal(t, 1, strings.Count(s, "diskLib := lib_disk.Library()"),
		"one construction for both bindings")
	require.Equal(t, 1, strings.Count(s, `{Field: "var.mode", Value: "420"}`),
		"specs are emitted once")
	require.Contains(t, s, `"disk": diskLib`)
	require.Contains(t, s, `"d": diskLib`)
}

func TestGenerateUBLibraryEmptyBodies(t *testing.T) {
	out, err := GenerateUBLibrary("empty", nil, nil, nil, nil)
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, "package empty")
	require.Contains(t, s, "func Library() *runtime.Library")
	require.NotContains(t, s, "Composites:", "no composite maps when there are no bodies")
}

func TestGenerateUBLibraryRejectsEmptyAlias(t *testing.T) {
	_, err := GenerateUBLibrary("", nil, nil, nil, nil)
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

	body := parseUB(t, "library.ub", `description: 'a cluster'

resources: { local.file.x: { path: '/tmp/x', content: 'hi', mode: 420 } }
`)
	bodies := map[string]*lang.File{"cluster": body}

	out, err := GenerateUBLibrary("net", resourceBodies(bodies), nil, nil, nil)
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
	fmt.Printf("resource-composites=%d\n", len(lib.ResourceComposites))
	for name, ct := range lib.ResourceComposites {
		fmt.Printf("composite=%s kind=%s body-fields=%d\n",
			name, ct.Kind, len(ct.Body.Body.Fields))
	}
	if lib.ResourceComposites["cluster"] == nil {
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
	require.Contains(t, got, "resource-composites=1")
	require.Contains(t, got, "composite=cluster kind=resource body-fields=2")
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
