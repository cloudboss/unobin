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

// allResource labels every name in bodies as a resource composite.
func allResource(bodies map[string]*lang.File) map[string]string {
	cats := make(map[string]string, len(bodies))
	for name := range bodies {
		cats[name] = "resource"
	}
	return cats
}

func TestGenerateUBLibraryProducesValidGo(t *testing.T) {
	body := parseUB(t, "resource-cluster.ub", `description: 'a cluster'

resources: { local.file.x: { path: '/tmp/x', content: 'hi', mode: 420 } }
`)
	bodies := map[string]*lang.File{"cluster": body}

	out, err := GenerateUBLibrary("net", bodies, allResource(bodies), nil, nil)
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "net.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", out)
}

func TestGenerateUBLibraryHasExpectedShape(t *testing.T) {
	bodies := map[string]*lang.File{
		"alpha": parseUB(t, "resource-alpha.ub", "description: 'a'"),
		"beta":  parseUB(t, "resource-beta.ub", "description: 'b'"),
	}

	out, err := GenerateUBLibrary("net", bodies, allResource(bodies), nil, nil)
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

func TestGenerateUBLibrarySplitsByKind(t *testing.T) {
	bodies := map[string]*lang.File{
		"box":    parseUB(t, "resource-box.ub", "description: 'r'"),
		"lookup": parseUB(t, "data-lookup.ub", "description: 'd'"),
		"run":    parseUB(t, "action-run.ub", "description: 'a'"),
	}
	kinds := map[string]string{"box": "resource", "lookup": "data", "run": "action"}

	out, err := GenerateUBLibrary("mixed", bodies, kinds, nil, nil)
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
	bodies := map[string]*lang.File{"x": parseUB(t, "resource-x.ub", "description: 'x'")}
	_, err := GenerateUBLibrary("net", bodies, map[string]string{"x": "widget"}, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown kind")
}

func TestGenerateUBLibraryEmitsPerCompositeLibraries(t *testing.T) {
	body := parseUB(t, "resource-greeting.ub", `description: 'a greeting'

resources: { helloer.hello.file: { message: var.message, path: var.path } }
`)
	bodies := map[string]*lang.File{"greeting": body}
	imports := map[string]map[string]string{
		"greeting": {
			"helloer": "github.com/cloudboss/unobin-libraries-scratch/ub/helloer",
			"local":   "github.com/cloudboss/unobin/pkg/libraries/local",
		},
	}

	out, err := GenerateUBLibrary("greeter", bodies, allResource(bodies), imports, nil)
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
	bodies := map[string]*lang.File{
		"alpha": parseUB(t, "resource-alpha.ub", `description: 'a'`),
		"beta":  parseUB(t, "resource-beta.ub", `description: 'b'`),
	}
	// Both composites import the same package; the generated file
	// should declare it once and bind both composite-local aliases to
	// the same Go-level identifier.
	imports := map[string]map[string]string{
		"alpha": {"local": "github.com/cloudboss/unobin/pkg/libraries/local"},
		"beta":  {"thing": "github.com/cloudboss/unobin/pkg/libraries/local"},
	}
	out, err := GenerateUBLibrary("test", bodies, allResource(bodies), imports, nil)
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
		"archive": parseUB(t, "resource-archive.ub", "description: 'a'"),
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

	out, err := GenerateUBLibrary("files", bodies, allResource(bodies), imports, goSpecs)
	require.NoError(t, err)

	want := `// Code generated by unobin. DO NOT EDIT.
package files

import (
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	lib_disk "github.com/example/disk"
	lib_plain "github.com/example/plain"
)

func Library() *runtime.Library {
	diskLib := lib_disk.Library()
	diskLib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.file": {
			{Kind: "predicate", Require: "var.path != null", Message: "a file needs a path"},
		},
	}
	diskLib.Defaults = map[string][]lang.DefaultSpec{
		"resource.file": {
			{Field: "var.mode", Value: "420"},
			{Field: "var.create-directory", Optional: true},
		},
	}
	return &runtime.Library{
		Name: "files",
		ResourceComposites: map[string]*runtime.CompositeType{
			"archive": {
				Name: "archive",
				Kind: runtime.NodeResource,
				Body: &lang.File{Kind: lang.FileUnknown, Path: "resource-archive.ub", Body: &lang.ObjectLit{Fields: []*lang.Field{{Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "description"}, Value: &lang.StringLit{Value: "a"}}}}},
				Libraries: map[string]*runtime.Library{
					"disk":  diskLib,
					"plain": lib_plain.Library(),
				},
			},
		},
	}
}
`
	require.Equal(t, want, string(out))
}

// TestGenerateUBLibrarySharesSpecsAcrossComposites proves two
// composites importing the same spec-bearing path bind one shared
// instance, so the specs are emitted once.
func TestGenerateUBLibrarySharesSpecsAcrossComposites(t *testing.T) {
	bodies := map[string]*lang.File{
		"alpha": parseUB(t, "resource-alpha.ub", "description: 'a'"),
		"beta":  parseUB(t, "resource-beta.ub", "description: 'b'"),
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

	out, err := GenerateUBLibrary("files", bodies, allResource(bodies), imports, goSpecs)
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

	body := parseUB(t, "resource-cluster.ub", `description: 'a cluster'

resources: { local.file.x: { path: '/tmp/x', content: 'hi', mode: 420 } }
`)
	bodies := map[string]*lang.File{"cluster": body}

	out, err := GenerateUBLibrary("net", bodies, allResource(bodies), nil, nil)
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
