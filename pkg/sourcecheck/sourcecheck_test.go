package sourcecheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/require"
)

func TestCheckFactoryReportsReferenceAndTypeErrors(t *testing.T) {
	err := checkFactoryFixture(t, "invalid/reference-type-error/factory", map[string]string{
		"example.com/definition": "definition",
	})

	requireErrorMatchesGolden(t, "invalid/reference-type-error/factory", err)
}

func TestCheckFactoryReportsLiteralConstraintErrors(t *testing.T) {
	err := checkFactoryFixture(t, "invalid/literal-constraint/factory", map[string]string{
		"example.com/constraints": "constraints",
	})

	requireErrorMatchesGolden(t, "invalid/literal-constraint/factory", err)
}

func TestCheckFactoryReportsForEachNesting(t *testing.T) {
	path := fixturePath("invalid/nested-foreach/factory")
	body := parseFactoryAt(t, path)
	resolver := newTestResolver(t, filepath.Dir(path))
	resolver.remotes["example.com/opaque"] = opaqueGoSource()

	_, err := CheckFactoryBody(body, Options{
		Resolver:    resolver,
		Versions:    map[string]string{"example.com/opaque": "v1.0.0"},
		SchemaCache: NewSchemaCache(),
		ProjectDir:  filepath.Dir(path),
	})

	requireErrorMatchesGolden(t, "invalid/nested-foreach/factory", err)
}

func TestCheckFactoryAcceptsSplitConfigPackage(t *testing.T) {
	path := fixturePath("valid/schema-dependencies/aws-split/factory")
	body := parseFactoryAt(t, path)
	resolver := newTestResolver(t, filepath.Dir(path))
	serviceSource := goFixtureSource(t, "configforward/service")
	serviceSource.ModulePath = "example.com/aws"
	serviceSource.GoImportPath = "example.com/aws/service"
	configSource := goFixtureSource(t, "configforward/config")
	configSource.ModulePath = "example.com/aws"
	configSource.GoImportPath = "example.com/aws/config"
	resolver.remotes["example.com/aws//service"] = serviceSource
	resolver.remotes["example.com/aws//config"] = configSource

	_, err := CheckFactoryBody(body, Options{
		Resolver: resolver,
		Versions: map[string]string{"example.com/aws": "v1.0.0"},
		Source: &resolve.Source{
			FS:   os.DirFS(filepath.Dir(path)),
			Path: filepath.Dir(path),
		},
	})

	require.NoError(t, err)
}

func TestCheckFactoryReadsSchemaDependencies(t *testing.T) {
	path := fixturePath("valid/schema-dependencies/check-factory/factory")
	body := parseFactoryAt(t, path)
	resolver := newTestResolver(t, filepath.Dir(path))
	schemaSource := goFixtureSource(t, "configschema")
	schemaSource.ModulePath = "example.com/schema"
	schemaSource.GoImportPath = "example.com/schema"
	resolver.remotes["example.com/schema"] = schemaSource

	_, err := CheckFactoryBody(body, Options{
		Resolver: resolver,
		Versions: map[string]string{"example.com/schema": "v1.0.0"},
		Source: &resolve.Source{
			FS:   os.DirFS(filepath.Dir(path)),
			Path: filepath.Dir(path),
		},
	})

	require.NoError(t, err)
}

func TestCheckUBLibraryReportsCompositeBodyErrors(t *testing.T) {
	root := fixtureDir(t, "invalid/library-body-error")
	resolver := newTestResolver(t, root)

	err := CheckUBLibrary(sourceForDir(t, root), Options{
		Resolver:    resolver,
		SchemaCache: NewSchemaCache(),
	})

	requireErrorMatchesGolden(t, "invalid/library-body-error/library", err)
}

func TestCheckDoesNotFetchWhenNoFetchResolverMissesRemote(t *testing.T) {
	path := fixturePath("valid/no-fetch-missing-remote/factory")
	body := parseFactoryAt(t, path)
	resolver := newTestResolver(t, filepath.Dir(path))
	resolver.noFetchMissing["example.com/missing"] = true

	result, err := CheckFactoryBody(body, Options{
		Resolver: resolver,
		Versions: map[string]string{"example.com/missing": "v1.0.0"},
		Mode:     ModeNoFetch,
	})

	require.NoError(t, err)
	require.Contains(t, result.Libraries, "ext")
}

func BenchmarkCheckFactoryBodyLargeGraph(b *testing.B) {
	body := largeFactoryBody(500)
	resolver := newTestResolver(b, sourcecheckFixtureRoot())
	resolver.remotes["example.com/opaque-a"] = opaqueGoSource()
	resolver.remotes["example.com/opaque-b"] = opaqueGoSource()
	opts := Options{
		Resolver: resolver,
		Versions: map[string]string{
			"example.com/opaque-a": "v1.0.0",
			"example.com/opaque-b": "v1.0.0",
		},
		SchemaCache: NewSchemaCache(),
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := CheckFactoryBody(body, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func checkFactoryFixture(
	t testing.TB,
	name string,
	goRemotes map[string]string,
) error {
	t.Helper()
	path := fixturePath(name)
	body := parseFactoryAt(t, path)
	resolver := newTestResolver(t, filepath.Dir(path))
	versions := map[string]string{}
	for module, fixture := range goRemotes {
		resolver.remotes[module] = goFixtureSource(t, fixture)
		versions[module] = "v1.0.0"
	}
	_, err := CheckFactoryBody(body, Options{
		Resolver:    resolver,
		Versions:    versions,
		SchemaCache: NewSchemaCache(),
		ProjectDir:  filepath.Dir(path),
	})
	return err
}

func requireErrorMatchesGolden(t testing.TB, name string, err error) {
	t.Helper()
	require.Error(t, err)
	golden := ubtest.ReadFixture(t, fixturePath(name)+".err")
	require.Equal(t, strings.TrimRight(golden, "\n"), strings.TrimRight(err.Error(), "\n"))
}

func parseFactoryAt(t testing.TB, path string) syntax.FactoryBody {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	file, err := syntax.ParseSource(path, body)
	require.NoError(t, err)
	require.NotNil(t, file.Factory)
	if errs := syntax.ValidateFile(file); errs.Len() > 0 {
		t.Fatalf("validate %s: %v", path, errs.Err())
	}
	return file.Factory.Body
}

func fixturePath(name string) string {
	return filepath.Join(sourcecheckFixtureRoot(), filepath.FromSlash(name+".ub"))
}

func fixtureDir(t testing.TB, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join(sourcecheckFixtureRoot(), filepath.FromSlash(name)))
	require.NoError(t, err)
	return abs
}

func sourcecheckFixtureRoot() string {
	return filepath.Join("testdata", "ub", "sourcecheck")
}

func sourceForDir(t testing.TB, dir string) *resolve.Source {
	t.Helper()
	abs, err := filepath.Abs(dir)
	require.NoError(t, err)
	return &resolve.Source{FS: os.DirFS(abs), Path: abs}
}

func goFixtureSource(t testing.TB, name string) *resolve.Source {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "goschema", "testdata", name))
	require.NoError(t, err)
	return &resolve.Source{
		FS:           os.DirFS(abs),
		Path:         abs,
		ModulePath:   "example.com/" + name,
		GoImportPath: "example.com/" + name,
	}
}

func opaqueGoSource() *resolve.Source {
	return &resolve.Source{
		FS: fstest.MapFS{
			"library.go": &fstest.MapFile{Data: []byte("package opaque\n")},
		},
	}
}

type testResolver struct {
	local          *resolve.LocalResolver
	remotes        map[string]*resolve.Source
	noFetchMissing map[string]bool
}

func newTestResolver(t testing.TB, root string) *testResolver {
	t.Helper()
	abs, err := filepath.Abs(root)
	require.NoError(t, err)
	return &testResolver{
		local:          resolve.NewLocalResolver(abs),
		remotes:        map[string]*resolve.Source{},
		noFetchMissing: map[string]bool{},
	}
}

func (r *testResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	source, ok, err := r.lookup(ref)
	if err != nil {
		return nil, err
	}
	if ok {
		return source, nil
	}
	return nil, fmt.Errorf("test resolver: no source for %s", remoteKey(ref))
}

func (r *testResolver) ResolveNoFetch(ref resolve.ImportRef) (*resolve.Source, bool, error) {
	if remote, ok := ref.(*resolve.RemoteImport); ok && r.noFetchMissing[remote.URL] {
		return nil, false, nil
	}
	return r.lookup(ref)
}

func (r *testResolver) lookup(ref resolve.ImportRef) (*resolve.Source, bool, error) {
	switch v := ref.(type) {
	case *resolve.LocalImport:
		source, err := r.local.Resolve(v)
		return source, err == nil, err
	case *resolve.RemoteImport:
		if v.Subdir != "" {
			if source, ok := r.remotes[v.URL+"//"+v.Subdir]; ok {
				return source, true, nil
			}
		}
		source, ok := r.remotes[v.URL]
		return source, ok, nil
	default:
		return nil, false, fmt.Errorf("unsupported import ref %T", ref)
	}
}

func remoteKey(ref resolve.ImportRef) string {
	if remote, ok := ref.(*resolve.RemoteImport); ok {
		values := []string{remote.URL}
		if remote.Subdir != "" {
			values = append(values, remote.Subdir)
		}
		if remote.Version != "" {
			values = append(values, remote.Version)
		}
		return strings.Join(values, "//")
	}
	return fmt.Sprintf("%T", ref)
}

func largeFactoryBody(nodes int) syntax.FactoryBody {
	body := syntax.FactoryBody{
		Imports: []syntax.ImportDecl{
			{
				Alias: syntax.Ident{Name: "a"},
				Ref:   &lang.StringLit{Value: "example.com/opaque-a"},
			},
			{
				Alias: syntax.Ident{Name: "b"},
				Ref:   &lang.StringLit{Value: "example.com/opaque-b"},
			},
		},
		Resources: make([]syntax.NodeDecl, 0, nodes),
	}
	for i := range nodes {
		alias := "a"
		if i%2 == 1 {
			alias = "b"
		}
		body.Resources = append(body.Resources, syntax.NodeDecl{
			Kind: syntax.NodeKind("resource"),
			Name: syntax.Ident{Name: fmt.Sprintf("r%03d", i)},
			Selector: syntax.NodeSelector{
				Alias:  syntax.Ident{Name: alias},
				Export: syntax.Ident{Name: "thing"},
			},
			Body: &lang.ObjectLit{Fields: []*lang.Field{}},
		})
	}
	return body
}
