package codegen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func BenchmarkGenerateUBLibraryMetadata(b *testing.B) {
	for _, count := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("composites=%d", count), func(b *testing.B) {
			workload := newEmissionWorkload(b, count)
			var output []byte
			for b.Loop() {
				var err error
				output, err = GenerateUBLibraryPackageWithAssetsAndConfigSchemas(
					"metadata", "metadata", workload.bodies, workload.imports,
					workload.specs, workload.sources, nil, workload.configs,
				)
				if err != nil {
					b.Fatal(err)
				}
			}
			file, err := parser.ParseFile(token.NewFileSet(), "metadata.go", output,
				parser.AllErrors)
			require.NoError(b, err, "%s", output)
			var exports []string
			calls := 0
			ast.Inspect(file, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok &&
						selector.Sel.Name == "Library" {
						calls++
					}
				}
				literal, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				mapping, ok := literal.Type.(*ast.MapType)
				if !ok {
					return true
				}
				pointer, ok := mapping.Value.(*ast.StarExpr)
				if !ok {
					return true
				}
				selector, ok := pointer.X.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "CompositeType" {
					return true
				}
				for _, element := range literal.Elts {
					entry, ok := element.(*ast.KeyValueExpr)
					require.True(b, ok)
					name, ok := entry.Key.(*ast.BasicLit)
					require.True(b, ok)
					exports = append(exports, name.Value)
				}
				return true
			})
			var expected []string
			for i := range count {
				expected = append(expected, fmt.Sprintf("\"box-%03d\"", i))
			}
			require.Equal(b, expected, exports)
			require.GreaterOrEqual(b, calls, 2)
			b.ReportMetric(float64(count), "composites")
			b.ReportMetric(20, "config-fields")
			b.ReportMetric(float64(len(output)), "bytes/output")
			b.ReportMetric(float64(calls), "library-calls/output")
		})
	}
}

type emissionWorkload struct {
	bodies  map[string]map[string]syntax.FactoryBody
	imports map[string]map[string]map[string]string
	specs   map[string]GoLibrarySpecs
	sources map[string]syntax.SourceFileSpec
	configs map[string]map[string]map[string]runtime.LibraryConfigSchema
}

func newEmissionWorkload(b *testing.B, count int) *emissionWorkload {
	b.Helper()
	source := ubtest.ReadValidFixture(b, "testdata/ub/emission", "library")
	file, err := syntax.ParseSource("library.ub", []byte(source))
	require.NoError(b, err)
	require.NotNil(b, file.Library)
	require.Len(b, file.Library.Exports, 1)
	bodies := map[string]syntax.FactoryBody{}
	imports := map[string]map[string]string{}
	configs := map[string]map[string]runtime.LibraryConfigSchema{}
	var fields []typecheck.ObjectField
	var defaults []lang.DefaultSpec
	for i := range 20 {
		name := fmt.Sprintf("field-%03d", i)
		fields = append(fields, typecheck.ObjectField{
			Name: name, Type: typecheck.TString(), Optional: true,
		})
		defaults = append(defaults, lang.DefaultSpec{Field: "input." + name, Optional: true})
	}
	constraints := []lang.ConstraintSpec{{
		Kind: "required-together", Fields: []string{"input.field-000", "input.field-001"},
	}}
	config := runtime.LibraryConfigSchema{
		Path: "example.com/core", Fields: fields, Defaults: defaults,
		Constraints: constraints, Identity: "core-config", Digest: "fixed-config",
	}
	for i := range count {
		name := fmt.Sprintf("box-%03d", i)
		bodies[name] = file.Library.Exports[0].Body
		imports[name] = map[string]string{
			"core": "example.com/core", "child": "benchmark-stack/internal/child",
		}
		configs[name] = map[string]runtime.LibraryConfigSchema{"example.com/core": config}
	}
	return &emissionWorkload{
		bodies:  map[string]map[string]syntax.FactoryBody{"resource": bodies},
		imports: map[string]map[string]map[string]string{"resource": imports},
		specs: map[string]GoLibrarySpecs{
			"example.com/core": {
				Defaults:    map[string][]lang.DefaultSpec{"resource.file": defaults},
				Constraints: map[string][]lang.ConstraintSpec{"resource.file": constraints},
				Schema: &runtime.LibrarySchema{
					Resources: map[string]*runtime.TypeSchema{
						"file": {SensitiveInputs: []string{"content"}},
					},
					HasConfiguration: true, ConfigurationFields: fields,
					ConfigurationDefaults: defaults, ConfigurationConstraints: constraints,
					ConfigurationIdentity: "core-config", ConfigurationDigest: "fixed-config",
				},
			},
		},
		sources: map[string]syntax.SourceFileSpec{
			"library.ub": {
				DisplayPath: "library.ub", LibraryPath: "example.com/metadata",
				ProjectRelPath: "library.ub", PackageRelPath: "library.ub",
				LineStarts: parse.LineStarts([]byte(source)),
			},
		},
		configs: map[string]map[string]map[string]runtime.LibraryConfigSchema{
			"resource": configs,
		},
	}
}
