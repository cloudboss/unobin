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

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func parseSyntaxUB(t *testing.T, kind, name, src string) syntax.FactoryBody {
	t.Helper()
	body := fmt.Appendf(nil, "%s: %s {\n%s\n}\n", name, kind, src)
	f, err := syntax.ParseSource("library.ub", body)
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
	body := parseSyntaxUB(t, "resource", "cluster", `description: 'a cluster'

resources: { x: local.file { path: '/tmp/x', content: 'hi', mode: 420 } }
`)

	out, err := GenerateUBLibrary(
		"net",
		resourceSyntaxBodies(map[string]syntax.FactoryBody{"cluster": body}),
		nil,
		nil,
	)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "net.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)
}

func TestGenerateUBLibrarySanitizesPackageName(t *testing.T) {
	body := parseSyntaxUB(t, "resource", "cluster", `description: 'a cluster'`)

	out, err := GenerateUBLibrary(
		"project-b",
		resourceSyntaxBodies(map[string]syntax.FactoryBody{"cluster": body}),
		nil,
		nil,
	)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "project-b.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)

	s := string(out)
	require.Contains(t, s, "package project_b")
	require.Contains(t, s, `Name: "project-b"`)
}

func TestGenerateUBLibraryHasExpectedForm(t *testing.T) {
	bodies := map[string]syntax.FactoryBody{
		"alpha": parseSyntaxUB(t, "resource", "alpha", "description: 'a'"),
		"beta":  parseSyntaxUB(t, "resource", "beta", "description: 'b'"),
	}

	out, err := GenerateUBLibrary("net", resourceSyntaxBodies(bodies), nil, nil)
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
	syntaxBody := parseSyntaxUB(t, "resource", "greeting", `
locals: { target: resource.helper.path }
resources: {
  helper: local.fs-file { path: '/tmp/helper' }
  file: local.fs-file { path: local.target }
}
`)

	out, err := GenerateUBLibrary(
		"net",
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
	require.NotContains(t, s, `Body: &lang.File{`)
}

func TestGenerateUBLibraryOmitsGenericBody(t *testing.T) {
	syntaxBody := parseSyntaxUB(t, "resource", "greeting", `
inputs: { path: { type: string } }
resources: { file: local.file { path: var.path } }
outputs: { path: { value: resource.file.path } }
`)

	out, err := GenerateUBLibrary(
		"net",
		resourceSyntaxBodies(map[string]syntax.FactoryBody{"greeting": syntaxBody}),
		nil,
		nil,
	)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "net.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)

	s := string(out)
	require.Regexp(t, `ResourceComposites:\s*map\[string\]\*runtime\.CompositeType\{`, s)
	require.Contains(t, s, `"greeting": {`)
	require.Contains(t, s, `SyntaxBody: &syntax.FactoryBody{`)
	require.NotContains(t, s, `Body: &lang.File{`)
}

func TestGenerateUBLibrarySplitsByKind(t *testing.T) {
	bodies := map[string]map[string]syntax.FactoryBody{
		"resource": {"box": parseSyntaxUB(t, "resource", "box", "description: 'r'")},
		"data":     {"lookup": parseSyntaxUB(t, "data", "lookup", "description: 'd'")},
		"action":   {"run": parseSyntaxUB(t, "action", "run", "description: 'a'")},
	}

	out, err := GenerateUBLibrary("mixed", bodies, nil, nil)
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
	bodies := map[string]syntax.FactoryBody{
		"x": parseSyntaxUB(t, "resource", "x", "description: 'x'"),
	}
	_, err := GenerateUBLibrary(
		"net",
		map[string]map[string]syntax.FactoryBody{"widget": bodies},
		nil,
		nil,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown kind")
}

func TestGenerateUBLibraryAllowsSameNameAcrossKinds(t *testing.T) {
	bodies := map[string]map[string]syntax.FactoryBody{
		"resource": {"vpc": parseSyntaxUB(t, "resource", "vpc", "description: 'r'")},
		"data":     {"vpc": parseSyntaxUB(t, "data", "vpc", "description: 'd'")},
		"action":   {"vpc": parseSyntaxUB(t, "action", "vpc", "description: 'a'")},
	}

	out, err := GenerateUBLibrary("net", bodies, nil, nil)
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
	body := parseSyntaxUB(t, "resource", "greeting", `description: 'a greeting'

resources: { file: helloer.hello { message: var.message, path: var.path } }
`)
	imports := map[string]map[string]string{
		"greeting": {
			"helloer": "github.com/example/helloer",
			"local":   "github.com/cloudboss/unobin/pkg/libraries/local",
		},
	}

	out, err := GenerateUBLibrary(
		"greeter",
		resourceSyntaxBodies(map[string]syntax.FactoryBody{"greeting": body}),
		compositeImports("resource", imports),
		nil,
	)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "greeter.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)

	s := string(out)
	require.Contains(t, s, `lib_helloer "github.com/example/helloer"`,
		"each unique composite-imported path gets its own Go-level alias")
	require.Contains(t, s, `lib_local "github.com/cloudboss/unobin/pkg/libraries/local"`)
	require.Regexp(t, `Libraries:\s*map\[string\]\*runtime\.Library\{`,
		s, "the composite keeps its resolved imports")
	require.Contains(t, s, `"helloer": lib_helloer.Library()`)
	require.Contains(t, s, `"local":   lib_local.Library()`)
}

func TestGenerateUBLibrarySharesIdentForSamePath(t *testing.T) {
	bodies := map[string]syntax.FactoryBody{
		"alpha": parseSyntaxUB(t, "resource", "alpha", "description: 'a'"),
		"beta":  parseSyntaxUB(t, "resource", "beta", "description: 'b'"),
	}
	imports := map[string]map[string]string{
		"alpha": {"local": "github.com/cloudboss/unobin/pkg/libraries/local"},
		"beta":  {"thing": "github.com/cloudboss/unobin/pkg/libraries/local"},
	}
	out, err := GenerateUBLibrary(
		"test",
		resourceSyntaxBodies(bodies),
		compositeImports("resource", imports),
		nil,
	)
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
	bodies := map[string]syntax.FactoryBody{
		"archive": parseSyntaxUB(t, "resource", "archive", "description: 'a'"),
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
		"files",
		resourceSyntaxBodies(bodies),
		compositeImports("resource", imports),
		goSpecs,
	)
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, `diskLib := lib_disk.Library()`)
	require.Contains(t, s, `diskLib.Constraints = map[string][]lang.ConstraintSpec{`)
	require.Contains(t, s, `{Kind: "predicate", Require: "var.path != null"`)
	require.Contains(t, s, `{Field: "var.mode", Value: "420"}`)
	require.Contains(t, s, `{Field: "var.create-directory", Optional: true}`)
	require.Regexp(t, `Name:\s*"archive"`, s)
	require.Regexp(t, `Kind:\s*runtime\.NodeResource`, s)
	require.Contains(t, s, `SyntaxBody: &syntax.FactoryBody{`)
	require.Contains(t, s, `"disk":  diskLib`)
	require.Contains(t, s, `"plain": lib_plain.Library()`)
}

// TestGenerateUBLibrarySharesSpecsAcrossComposites proves two
// composites importing the same spec-bearing path bind one shared
// instance, so the specs are emitted once.
func TestGenerateUBLibrarySharesSpecsAcrossComposites(t *testing.T) {
	bodies := map[string]syntax.FactoryBody{
		"alpha": parseSyntaxUB(t, "resource", "alpha", "description: 'a'"),
		"beta":  parseSyntaxUB(t, "resource", "beta", "description: 'b'"),
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
		"files",
		resourceSyntaxBodies(bodies),
		compositeImports("resource", imports),
		goSpecs,
	)
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
	out, err := GenerateUBLibrary("empty", nil, nil, nil)
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, "package empty")
	require.Contains(t, s, "func Library() *runtime.Library")
	require.NotContains(t, s, "Composites:", "no composite maps when there are no bodies")
}

func TestGenerateUBLibraryRejectsEmptyAlias(t *testing.T) {
	_, err := GenerateUBLibrary("", nil, nil, nil)
	require.Error(t, err)
}

// TestGenerateUBLibraryCompilesWithCaller writes the generated Go
// source into a temporary Go library, alongside a small main that
// imports the package and prints attributes from Library() that the
// test then asserts on. The temporary library has a replace directive
// pointing back at the unobin checkout, so `go build` can resolve
// the runtime and syntax imports without going to the network.
func TestGenerateUBLibraryCompilesWithCaller(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped: spawns `go run` and is slow")
	}

	body := parseSyntaxUB(t, "resource", "cluster", `description: 'a cluster'

resources: { x: local.file { path: '/tmp/x', content: 'hi', mode: 420 } }
`)

	out, err := GenerateUBLibrary(
		"net",
		resourceSyntaxBodies(map[string]syntax.FactoryBody{"cluster": body}),
		nil,
		nil,
	)
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
		resourceCount := 0
		if ct.SyntaxBody != nil {
			resourceCount = len(ct.SyntaxBody.Resources)
		}
		fmt.Printf("composite=%s kind=%s resources=%d\n",
			name, ct.Kind, resourceCount)
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
	require.Contains(t, got, "composite=cluster kind=resource resources=1")
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
