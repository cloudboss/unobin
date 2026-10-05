package sourcecheck

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestAnalysisScalingFixtures(t *testing.T) {
	for _, count := range []int{1, 10, 100} {
		t.Run(fmt.Sprintf("exports=%d", count), func(t *testing.T) {
			fixture := newAnalysisBenchmark(t,
				fmt.Sprintf("valid/analysis-scaling/exports/count-%d/factory", count))
			result, err := CheckFactoryBody(*fixture.options.Body, Options{
				Source: fixture.options.Source, Resolver: fixture.options.Resolver,
				Versions: fixture.options.Versions,
				SchemaCache: NewSchemaCacheWithReader(
					func(string) (*runtime.LibrarySchema, []string, error) {
						return importAnalysisSchema(), nil, nil
					}),
			})
			require.NoError(t, err)
			var addresses []string
			for i := range count {
				address := fmt.Sprintf("resource.node-%03d", i)
				addresses = append(addresses, address, address+"/resource.file",
					address+"/library-config.std")
			}
			require.Equal(t, slices.Sorted(slices.Values(addresses)),
				slices.Sorted(maps.Keys(result.DAG.Nodes)))
		})
	}
}

func BenchmarkAnalyzeNestedLibraries(b *testing.B) {
	for _, depth := range []int{2, 5, 10} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			fixture := newAnalysisBenchmark(b,
				fmt.Sprintf("valid/analysis-scaling/nested/depth-%d/factory", depth))
			var analysis *ImportAnalysis
			for b.Loop() {
				analysis = fixture.analyze(b)
			}
			library := analysis.Libraries["next"]
			for i := range depth {
				composite := library.Composite(runtime.NodeResource, "box")
				require.NotNil(b, composite)
				if i == depth-1 {
					require.Equal(b, importAnalysisSchema(), composite.Libraries["std"].Schema)
				} else {
					library = composite.Libraries["next"]
					require.NotNil(b, library)
				}
			}
			fixture.report(b)
			b.ReportMetric(float64(depth), "libraries")
		})
	}
}

func BenchmarkAnalyzeSharedExports(b *testing.B) {
	for _, count := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("exports=%d", count), func(b *testing.B) {
			fixture := newAnalysisBenchmark(b,
				fmt.Sprintf("valid/analysis-scaling/exports/count-%d/factory", count))
			var analysis *ImportAnalysis
			for b.Loop() {
				analysis = fixture.analyze(b)
			}
			library := analysis.Libraries["shared"]
			require.NotNil(b, library)
			require.Len(b, library.ResourceComposites, count)
			for i := range count {
				composite := library.Composite(runtime.NodeResource, fmt.Sprintf("box-%03d", i))
				require.NotNil(b, composite)
				require.Equal(b, importAnalysisSchema(), composite.Libraries["std"].Schema)
			}
			fixture.report(b)
			b.ReportMetric(float64(count), "exports")
		})
	}
}

type analysisBenchmark struct {
	refs        map[string]resolve.ImportRef
	options     ImportAnalysisOptions
	ubReads     int
	schemaReads int
	resolves    int
}

func newAnalysisBenchmark(t testing.TB, fixture string) *analysisBenchmark {
	t.Helper()
	path := fixturePath(fixture)
	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS(filepath.Dir(path))))
	body := parseFactoryAtPath(t, filepath.Join(root, filepath.Base(path)))
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	require.Empty(t, errs)
	goSource := writeImportAnalysisGoLibrary(t)
	resolver := newTestResolver(t, root)
	resolver.remotes["example.com/schema"] = &resolve.Source{
		FS: os.DirFS(goSource), Path: goSource,
		ModulePath: "example.com/schema", GoImportPath: "example.com/schema",
	}
	versions := map[string]string{"example.com/schema": "v1.0.0"}
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		url := "example.com/analysis"
		if after, ok := strings.CutPrefix(name, "level-"); ok {
			url += "-" + after
		} else if name != "library" {
			continue
		}
		path := filepath.Join(root, name)
		resolver.remotes[url] = &resolve.Source{FS: os.DirFS(path), Path: path}
		versions[url] = "v1.0.0"
	}
	benchmark := &analysisBenchmark{refs: refs}
	benchmark.options = ImportAnalysisOptions{
		Resolver: analysisBenchmarkResolver{
			wrapped: resolver, reads: &benchmark.ubReads, calls: &benchmark.resolves,
		},
		Source: &resolve.Source{
			FS: analysisBenchmarkFS{FS: os.DirFS(root), reads: &benchmark.ubReads}, Path: root,
		},
		Versions: versions, Body: &body,
	}
	return benchmark
}

func (fixture *analysisBenchmark) analyze(b *testing.B) *ImportAnalysis {
	b.Helper()
	opts := fixture.options
	opts.SchemaCache = NewSchemaCacheWithReader(
		func(string) (*runtime.LibrarySchema, []string, error) {
			fixture.schemaReads++
			return importAnalysisSchema(), nil, nil
		})
	analysis, err := AnalyzeImports(fixture.refs, opts)
	require.NoError(b, err)
	return analysis
}

func (fixture *analysisBenchmark) report(b *testing.B) {
	b.Helper()
	require.Equal(b, b.N, fixture.schemaReads)
	b.ReportMetric(float64(fixture.ubReads)/float64(b.N), "ub-reads/op")
	b.ReportMetric(float64(fixture.schemaReads)/float64(b.N), "schema-reads/op")
	b.ReportMetric(float64(fixture.resolves)/float64(b.N), "resolves/op")
}

type analysisBenchmarkFS struct {
	fs.FS
	reads *int
}

func (source analysisBenchmarkFS) Open(name string) (fs.File, error) {
	if strings.HasSuffix(name, ".ub") {
		*source.reads++
	}
	return source.FS.Open(name)
}

type analysisBenchmarkResolver struct {
	wrapped resolve.Resolver
	reads   *int
	calls   *int
}

func (resolver analysisBenchmarkResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	return resolver.ResolveFrom(ref, nil)
}

func (resolver analysisBenchmarkResolver) ResolveFrom(
	ref resolve.ImportRef, parent *resolve.Source,
) (*resolve.Source, error) {
	*resolver.calls++
	source, err := resolve.ResolveImportFrom(resolver.wrapped, ref, parent)
	if err != nil {
		return nil, err
	}
	copy := *source
	copy.FS = analysisBenchmarkFS{FS: source.FS, reads: resolver.reads}
	return &copy, nil
}

func BenchmarkSchemaCacheReads(b *testing.B) {
	for _, invalidated := range []bool{false, true} {
		name := "unchanged"
		if invalidated {
			name = "helper-edit"
		}
		b.Run(name, func(b *testing.B) {
			benchmarkSchemaCacheReads(b, invalidated)
		})
	}
}

func benchmarkSchemaCacheReads(b *testing.B, invalidated bool) {
	b.Helper()
	dir := writeImportAnalysisGoLibrary(b)
	before, err := os.ReadFile("testdata/golibrary/analysis-helper/helper.go")
	require.NoError(b, err)
	after, err := os.ReadFile("testdata/golibrary/analysis-helper/helper-updated.go.txt")
	require.NoError(b, err)
	helper := filepath.Join(dir, "helper.go")
	require.NoError(b, os.WriteFile(helper, before, 0o644))
	reads := 0
	reader := func(string) (*runtime.LibrarySchema, []string, error) {
		reads++
		body, err := os.ReadFile(helper)
		return importAnalysisSchema(), []string{string(body)}, err
	}
	cache := NewSchemaCacheWithReader(reader)
	_, _, err = cache.Read(dir)
	require.NoError(b, err)
	reads = 0
	var warnings []string
	lookups := 100
	if invalidated {
		lookups = 2
	}
	for b.Loop() {
		if invalidated {
			require.NoError(b, os.WriteFile(helper, before, 0o644))
			cache = NewSchemaCacheWithReader(reader)
		}
		for i := range lookups {
			if invalidated && i == 1 {
				require.NoError(b, os.WriteFile(helper, after, 0o644))
			}
			var schema *runtime.LibrarySchema
			schema, warnings, err = cache.Read(dir)
			if err != nil {
				b.Fatal(err)
			}
			if schema == nil {
				b.Fatal("missing schema")
			}
		}
	}
	expected := before
	if invalidated {
		expected = after
		require.Equal(b, b.N*2, reads)
	} else {
		require.Zero(b, reads)
	}
	require.Equal(b, []string{string(expected)}, warnings)
	b.ReportMetric(float64(lookups), "lookups")
	b.ReportMetric(float64(reads)/float64(b.N), "schema-reads/op")
}
