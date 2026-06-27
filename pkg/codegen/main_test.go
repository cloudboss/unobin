package codegen

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func testMainInput(t testing.TB, body, factoryName string) Input {
	t.Helper()
	src := []byte(sourceMainFactory(body))
	sf, err := syntax.ParseSource("factory.ub", src)
	require.NoError(t, err)
	require.NotNil(t, sf.Factory)
	return Input{
		FactoryBody: sf.Factory.Body,
		FactorySource: syntax.SourceFileSpec{
			DisplayPath:    "factory.ub",
			ProjectRelPath: "factory.ub",
			LineStarts:     parse.LineStarts(src),
		},
		FactoryName: factoryName,
	}
}

func sourceMainFactory(body string) string {
	if strings.HasPrefix(strings.TrimSpace(body), "factory"+":") {
		return body
	}
	return "factory" + ": {\n" + body + "\n}\n"
}

func TestGenerateInjectsGoConstraints(t *testing.T) {
	out, err := Generate(Input{
		Body:        "description: 'x'",
		FactoryName: "demo",
		GoImports: map[string]string{
			"aws": "github.com/example/aws",
		},
		GoConstraints: map[string]map[string][]lang.ConstraintSpec{
			"aws": {
				"resource.vpc": {
					{Kind: "exactly-one-of", Fields: []string{"cidr-block", "cidr-blocks"}},
					{
						Kind:    "predicate",
						When:    "(input.tier == 'prod')",
						Require: "(input.backups == true)",
						Message: "prod needs backups",
					},
					{
						Kind:    "predicate",
						When:    "true",
						Require: "(@t.value.weight <= 999)",
						ForEachLevels: []lang.ForEachSpecLevel{
							{Name: "@rule", In: "input.rules"},
							{Name: "@t", In: "@rule.value.targets"},
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, `libraries["aws"].Constraints = map[string][]lang.ConstraintSpec{`)
	require.Contains(t, s, `{Kind: "exactly-one-of", Fields: []string{"cidr-block", "cidr-blocks"}}`)
	require.Contains(t, s, `FactoryBody:     &factoryBody,`)
}

func TestGenerateInjectsGoDefaults(t *testing.T) {
	out, err := Generate(Input{
		Body:        "description: 'x'",
		FactoryName: "demo",
		GoImports: map[string]string{
			"local": "github.com/example/local",
		},
		GoDefaults: map[string]map[string][]lang.DefaultSpec{
			"local": {
				"resource.file": {
					{Field: "input.mode", Value: "420"},
					{Field: "input.create-directory", Optional: true},
				},
			},
		},
	})
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, `libraries["local"].Defaults = map[string][]lang.DefaultSpec{`)
	require.Contains(t, s, `{Field: "input.mode", Value: "420"}`)
	require.Contains(t, s, `{Field: "input.create-directory", Optional: true}`)
	require.Contains(t, s, `FactoryBody:     &factoryBody,`)
}

func TestGenerateInjectsGoSchemaSensitivity(t *testing.T) {
	out, err := Generate(Input{
		Body:        "description: 'x'",
		FactoryName: "demo",
		GoImports: map[string]string{
			"vault": "github.com/example/vault",
		},
		GoSchemas: map[string]*runtime.LibrarySchema{
			"vault": {
				Actions: map[string]*runtime.TypeSchema{
					"secret": {
						SensitiveInputs:  []string{"token"},
						SensitiveOutputs: []string{"value"},
					},
				},
			},
		},
	})
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, `libraries["vault"].Schema = &runtime.LibrarySchema{`)
	require.Contains(t, s,
		`"secret": {SensitiveInputs: []string{"token"}, SensitiveOutputs: []string{"value"}}`)
	require.Contains(t, s, `FactoryBody:     &factoryBody,`)
}

func TestGenerateInjectsConstraintsAndDefaultsTogether(t *testing.T) {
	out, err := Generate(Input{
		Body:        "description: 'x'",
		FactoryName: "demo",
		GoImports: map[string]string{
			"aws": "github.com/example/aws",
		},
		GoConstraints: map[string]map[string][]lang.ConstraintSpec{
			"aws": {
				"resource.vpc": {
					{Kind: "exactly-one-of", Fields: []string{"cidr-block", "cidr-blocks"}},
				},
			},
		},
		GoDefaults: map[string]map[string][]lang.DefaultSpec{
			"aws": {
				"resource.vpc": {
					{Field: "input.tier", Value: "'dev'"},
				},
			},
		},
	})
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, `libraries["aws"].Constraints = map[string][]lang.ConstraintSpec{`)
	require.Contains(t, s, `libraries["aws"].Defaults = map[string][]lang.DefaultSpec{`)
	require.Contains(t, s, `{Field: "input.tier", Value: "'dev'"}`)
	require.Contains(t, s, `FactoryBody:     &factoryBody,`)
}

func TestGenerateValidGo(t *testing.T) {
	out, err := Generate(Input{
		Body:        "actions" + ": { hi: core.command { argv: ['echo', 'world'] } }",
		FactoryName: "demo",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/libraries/core",
		},
	})
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "main.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", string(out))
}

func TestGenerateSanitizesImportAliases(t *testing.T) {
	out, err := Generate(Input{
		Body:        "description: 'x'\n",
		FactoryName: "demo",
		GoImports: map[string]string{
			"std-lib": "github.com/cloudboss/unobin-library-std",
		},
		UBImports: map[string]string{
			"project-b": "demo/internal/project-b",
		},
	})
	require.NoError(t, err)

	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "main.go", out, parser.AllErrors)
	require.NoError(t, err, "generated source should parse:\n%s", string(out))

	s := string(out)
	require.Contains(t, s, `"std-lib": runtime.LibraryWithPath(`)
	require.Contains(t, s, `lib_std_lib.Library(),`)
	require.Contains(t, s, `"github.com/cloudboss/unobin-library-std",`)
	require.Contains(t, s, `"project-b": runtime.LibraryWithPath(`)
	require.Contains(t, s, `lib_project_b.Library(),`)
	require.Contains(t, s, `"demo/internal/project-b",`)
}

func TestGenerateEmbedsFactoryName(t *testing.T) {
	out, err := Generate(Input{
		Body:        "description: 'x'\n",
		FactoryName: "my-factory",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/libraries/core",
		},
	})
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, `factoryName        = "my-factory"`)
}

func TestGenerateDeclaresStampVars(t *testing.T) {
	out, err := Generate(Input{
		Body:        "description: 'x'\n",
		FactoryName: "my-factory",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/libraries/core",
		},
	})
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s,
		"var (\n\tfactoryVersion  string\n\tcontentRevision string\n\tunobinVersion   string\n)")
}

func TestGenerateDoesNotEmbedBodyVerbatim(t *testing.T) {
	src := "description: 'x'"
	out, err := Generate(Input{
		Body:        src,
		FactoryName: "x",
		GoImports:   map[string]string{"core": "github.com/cloudboss/unobin/pkg/libraries/core"},
	})
	require.NoError(t, err)

	require.NotContains(t, string(out), strconv.Quote(src))
}

func TestGenerateOrdersImports(t *testing.T) {
	out, err := Generate(Input{
		Body:        "description: 'x'\n",
		FactoryName: "x",
		GoImports: map[string]string{
			"net":  "github.com/me/libraries/network",
			"aws":  "github.com/cloudboss/unobin-libraries/aws",
			"core": "github.com/cloudboss/unobin/pkg/libraries/core",
		},
	})
	require.NoError(t, err)

	s := string(out)
	awsAt := strings.Index(s, `"github.com/cloudboss/unobin-libraries/aws"`)
	coreAt := strings.Index(s, `"github.com/cloudboss/unobin/pkg/libraries/core"`)
	netAt := strings.Index(s, `"github.com/me/libraries/network"`)
	require.True(t, awsAt > 0 && coreAt > 0 && netAt > 0,
		"all imports should appear in source")
	require.Less(t, awsAt, coreAt, "aws should appear before core")
	require.Less(t, coreAt, netAt, "core should appear before net")
}

func TestGenerateRequiresFactoryName(t *testing.T) {
	_, err := Generate(Input{
		Body:      "description: 'x'",
		GoImports: map[string]string{"core": "github.com/cloudboss/unobin/pkg/libraries/core"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "FactoryName")
}

func TestGenerateImportsAndCallsUBLibraries(t *testing.T) {
	out, err := Generate(Input{
		Body:        "description: 'x'",
		FactoryName: "demo",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/libraries/core",
		},
		UBImports: map[string]string{
			"net":     "demo/internal/net",
			"cluster": "demo/internal/cluster",
		},
	})
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, `"core": runtime.LibraryWithPath(`)
	require.Contains(t, s, `lib_core.Library(),`)
	require.Contains(t, s, `"cluster": runtime.LibraryWithPath(`)
	require.Contains(t, s, `lib_cluster.Library(),`)
	require.Contains(t, s, `"net": runtime.LibraryWithPath(`)
	require.Contains(t, s, `lib_net.Library(),`)
	require.Contains(t, s, `FactoryBody:     &factoryBody,`)
}

func TestGenerateBuildsLibrariesMap(t *testing.T) {
	out, err := Generate(Input{
		Body:        "description: 'x'",
		FactoryName: "x",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/libraries/core",
			"aws":  "github.com/cloudboss/unobin-libraries/aws",
		},
	})
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, `"aws": runtime.LibraryWithPath(`)
	require.Contains(t, s, `lib_aws.Library(),`)
	require.Contains(t, s, `"github.com/cloudboss/unobin-libraries/aws",`)
	require.Contains(t, s, `"core": runtime.LibraryWithPath(`)
	require.Contains(t, s, `lib_core.Library(),`)
	require.Contains(t, s, `"github.com/cloudboss/unobin/pkg/libraries/core",`)
}
