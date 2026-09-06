package codegen

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func TestFactoryCatalogIncludesNestedLibrariesOnce(t *testing.T) {
	linked, err := linkFactoryCatalog(Input{
		CatalogImports: map[string]string{
			"example.com/shared": "example.com/shared",
			"example.com/wrap":   "factory/internal/wrap",
		},
		LibraryBindings: map[string]string{
			"first": "example.com/wrap", "second": "example.com/wrap",
		},
		CatalogSpecs: map[string]GoLibrarySpecs{
			"example.com/shared": {Defaults: map[string][]lang.DefaultSpec{
				"resource.file": {{Field: "input.mode", Value: "'0644'"}},
			}},
		},
	})
	require.NoError(t, err)
	require.Len(t, linked.Imports, 2)
	require.Len(t, linked.Registrations, 2)
	require.Equal(t, "example.com/shared", linked.Registrations[0].LibraryPath)
	require.Contains(t, linked.Registrations[0].Defaults, `"resource.file"`)
	require.Equal(t, "example.com/wrap", linked.Registrations[1].LibraryPath)
	require.Equal(t, "factory/internal/wrap", linked.Imports[1].Path)
	require.Len(t, linked.Bindings, 2)
	require.Equal(t, linked.Bindings[0].Path, linked.Bindings[1].Path)
	require.Equal(t, "example.com/wrap", linked.Bindings[0].Path)
	require.True(t, linked.HasLang)
}

func TestFactoryCatalogRejectsMissingLibrary(t *testing.T) {
	_, err := linkFactoryCatalog(Input{
		CatalogImports:  map[string]string{},
		LibraryBindings: map[string]string{"missing": "example.com/missing"},
	})
	require.ErrorContains(t, err, `library "example.com/missing" is unavailable`)
}

func TestFactoryCatalogRejectsReusedGoPackage(t *testing.T) {
	_, err := linkFactoryCatalog(Input{
		CatalogImports: map[string]string{
			"example.com/one": "factory/internal/shared",
			"example.com/two": "factory/internal/shared",
		},
	})
	require.ErrorContains(t, err, `Go package "factory/internal/shared" has multiple library paths`)
}

func TestFactoryCatalogRejectsConflictingRootImport(t *testing.T) {
	_, err := linkFactoryCatalog(Input{
		GoImports:       map[string]string{"cloud": "example.com/declared"},
		CatalogImports:  map[string]string{"example.com/cloud": "example.com/actual"},
		LibraryBindings: map[string]string{"cloud": "example.com/cloud"},
	})
	require.ErrorContains(t, err, `import "cloud" does not match its catalog registration`)
}

func TestFactoryCatalogRejectsMetadataWithoutRegistration(t *testing.T) {
	_, err := linkFactoryCatalog(Input{
		CatalogImports: map[string]string{},
		CatalogSpecs:   map[string]GoLibrarySpecs{"example.com/missing": {}},
	})
	require.ErrorContains(t, err, `metadata library "example.com/missing" is unavailable`)
}

func TestGenerateCatalogEmbedsNestedLibrarySpecs(t *testing.T) {
	fields := []typecheck.ObjectField{{Name: "region", Type: typecheck.TString()}}
	digest := cfg.DigestView(fields, nil, nil)
	out, err := Generate(Input{
		FactoryName:    "files",
		CatalogImports: map[string]string{"example.com/disk": "example.com/disk"},
		CatalogSpecs: map[string]GoLibrarySpecs{
			"example.com/disk": {
				Constraints: map[string][]lang.ConstraintSpec{
					"resource.file": {{Kind: "predicate", Require: "input.path != null"}},
				},
				Defaults: map[string][]lang.DefaultSpec{
					"resource.file": {{Field: "input.mode", Value: "420"}},
				},
				Schema: &runtime.LibrarySchema{
					Resources: map[string]*runtime.TypeSchema{
						"file": {SensitiveInputs: []string{"content"}},
					},
					HasConfiguration: true, ConfigurationFields: fields, ConfigurationDigest: digest,
				},
			},
		},
	})
	require.NoError(t, err)
	s := string(out)
	require.Contains(t, s, `library.Constraints = map[string][]lang.ConstraintSpec{`)
	require.Contains(t, s, `{Kind: "predicate", Require: "input.path != null"`)
	require.Contains(t, s, `{Field: "input.mode", Value: "420"}`)
	require.Contains(t, s, `"file": {SensitiveInputs: []string{"content"}}`)
	require.Contains(t, s, `HasConfiguration: true`)
	require.Contains(t, s, `ConfigurationDigest: "`+digest+`"`)
	require.Contains(t, s, `LibraryBindings: map[string]string{}`)
	_, err = parser.ParseFile(token.NewFileSet(), "main.go", out, parser.AllErrors)
	require.NoError(t, err)
}

func TestGenerateCatalogSharesSpecsAcrossAliases(t *testing.T) {
	out, err := Generate(Input{
		FactoryName:     "files",
		CatalogImports:  map[string]string{"example.com/disk": "example.com/disk"},
		LibraryBindings: map[string]string{"disk": "example.com/disk", "d": "example.com/disk"},
		CatalogSpecs: map[string]GoLibrarySpecs{
			"example.com/disk": {Defaults: map[string][]lang.DefaultSpec{
				"resource.file": {{Field: "input.mode", Value: "420"}},
			}},
		},
	})
	require.NoError(t, err)
	s := string(out)
	require.Equal(t, 1, strings.Count(s, `.Library()`))
	require.Equal(t, 1, strings.Count(s, `{Field: "input.mode", Value: "420"}`))
	require.Contains(t, s, `"d":    "example.com/disk"`)
	require.Contains(t, s, `"disk": "example.com/disk"`)
}
