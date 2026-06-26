package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func libraryConfigTestFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/check-library-configs", name)
}

func libraryConfigTestLibrary() *Library {
	return &Library{Schema: &LibrarySchema{
		HasConfiguration: true,
		ConfigurationFields: []typecheck.ObjectField{
			{Name: "region", Type: typecheck.TString()},
		},
	}}
}

func libraryConfigTestDAG(t *testing.T, name string, libs map[string]*Library) *DAG {
	t.Helper()
	body := parseSyntaxFactoryFixture(t, libraryConfigTestFixture(t, name)).body
	return BuildSyntaxDAG(body, libs)
}

func libraryConfigTestComposite(t *testing.T, name string) *syntaxRuntimeFixture {
	t.Helper()
	fixture := parseSyntaxCompositeFixture(t, libraryConfigTestFixture(t, name))
	return &fixture
}

func TestMissingLibraryConfigsRootScope(t *testing.T) {
	libs := map[string]*Library{"aws": libraryConfigTestLibrary()}
	dag := libraryConfigTestDAG(t, "root-missing", libs)

	require.Equal(t, []MissingLibraryConfig{{
		Address: "resource.main",
		Alias:   "aws",
	}}, MissingLibraryConfigs(dag, libs))

	dag = libraryConfigTestDAG(t, "root-configured", libs)

	require.Empty(t, MissingLibraryConfigs(dag, libs))
}

func TestMissingLibraryConfigsCompositeScope(t *testing.T) {
	configLib := libraryConfigTestLibrary()
	composite := libraryConfigTestComposite(t, "composite-missing").body
	compositeType := &CompositeType{Name: "app", Kind: NodeResource, SyntaxBody: &composite}
	compositeType.Libraries = map[string]*Library{"e2e": configLib}
	libs := map[string]*Library{"bundle": {
		ResourceComposites: map[string]*CompositeType{"app": compositeType},
	}}
	dag := libraryConfigTestDAG(t, "composite-call", libs)

	require.Equal(t, []MissingLibraryConfig{{
		Address: "resource.app/resource.file",
		Alias:   "e2e",
	}}, MissingLibraryConfigs(dag, libs))

	configuredComposite := libraryConfigTestComposite(t, "composite-configured").body
	configuredCompositeType := &CompositeType{
		Name:       "app",
		Kind:       NodeResource,
		SyntaxBody: &configuredComposite,
		Libraries:  map[string]*Library{"e2e": configLib},
	}
	libs = map[string]*Library{"bundle": {
		ResourceComposites: map[string]*CompositeType{"app": configuredCompositeType},
	}}
	dag = libraryConfigTestDAG(t, "composite-call", libs)

	require.Empty(t, MissingLibraryConfigs(dag, libs))
}

func TestCheckLibraryConfigsUsesSharedDiscovery(t *testing.T) {
	libs := map[string]*Library{"aws": libraryConfigTestLibrary()}
	dag := libraryConfigTestDAG(t, "root-missing", libs)

	err := (&Executor{DAG: dag, Libraries: libs}).CheckLibraryConfigs()
	require.EqualError(t, err, `resource.main: library "aws" requires library-configs.aws`)
}
