package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type configurationLookup struct{}

func configurationScopeFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadFixture(t, filepath.Join("testdata/ub/configuration-scope/valid", name))
}

func (*configurationLookup) Run(_ context.Context, config any) (*configEchoOutput, error) {
	return &configEchoOutput{Endpoint: endpointOf(config)}, nil
}

func (*configurationLookup) Read(_ context.Context, config any) (*configEchoOutput, error) {
	return &configEchoOutput{Endpoint: endpointOf(config)}, nil
}

func configurationScopeLibrary(t *testing.T, category Category, nested bool) *Library {
	t.Helper()
	libs := configuredLibraries()
	libs["fix"].Actions = map[string]ActionRegistration{
		"config-echo": MakeAction[configurationLookup, *configEchoOutput, any](),
	}
	libs["fix"].DataSources = map[string]DataSourceRegistration{
		"config-echo": MakeDataSource[configurationLookup, *configEchoOutput, any](),
	}
	block := configurationCategoryBlock(category)
	body := fmt.Sprintf(configurationScopeFixture(t, "body.ub.tmpl"), block, category)
	composite := syntaxComposite(t, "app", category, body)
	composite.Libraries = libs
	library := &Library{}
	library.AddComposite(composite)
	if !nested {
		return library
	}
	outerBody := fmt.Sprintf(configurationScopeFixture(t, "outer.ub.tmpl"), block, category)
	outer := syntaxComposite(t, "app", category, outerBody)
	outer.Libraries = map[string]*Library{"inner": library}
	result := &Library{}
	result.AddComposite(outer)
	return result
}

func configurationCategoryBlock(category Category) string {
	switch category {
	case NodeResource:
		return "resources"
	case NodeDataSource:
		return "data-sources"
	default:
		return "actions"
	}
}

func scopedConfigurationExecutor(
	t *testing.T, source string, libs map[string]*Library,
) *Executor {
	t.Helper()
	exec := configurationTestExecutor(t, source, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "config-scopes", Version: "v0", ContentRevision: "c0"}
	return exec
}

func TestCompositeConfigurationUsesEnclosingScope(t *testing.T) {
	for _, category := range []Category{NodeResource, NodeDataSource, NodeAction} {
		for _, nested := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/nested=%t", category, nested), func(t *testing.T) {
				libs := configuredLibraries()
				libs["wrapper"] = configurationScopeLibrary(t, category, nested)
				source := fmt.Sprintf(configurationScopeFixture(t, "factory.ub.tmpl"),
					configurationCategoryBlock(category), category, category)
				exec := scopedConfigurationExecutor(t, source, libs)
				exec.Inputs = map[string]any{"endpoint": "ROOT"}
				result, err := planAndApply(exec)
				require.NoError(t, err)
				require.Equal(t, map[string]any{"a": "A", "b": "B"}, result.Outputs)
			})
		}
	}
}

func TestNewEvalScopeSeparatesBodyEnvironments(t *testing.T) {
	rootBody := syntaxFactoryBody(t, configurationScopeFixture(t, "root-locals.ub"))
	child := syntaxResourceComposite(t, "app", configurationScopeFixture(t, "child-locals.ub"))
	childLibs := map[string]*Library{"child": {}}
	boundary := &Node{CompositeSyntaxBody: child.SyntaxBody, Libraries: childLibs}
	exec := &Executor{Libraries: map[string]*Library{"root": {}}, SyntaxSource: &rootBody}

	root, err := exec.newEvalScope(nil, map[string]any{"endpoint": "ROOT"})
	require.NoError(t, err)
	first, err := exec.newEvalScope(boundary, map[string]any{"endpoint": "A"})
	require.NoError(t, err)
	second, err := exec.newEvalScope(boundary, map[string]any{"endpoint": "B"})
	require.NoError(t, err)
	require.Equal(t, exec.Libraries, root.Libraries)
	require.Equal(t, childLibs, first.Libraries)
	require.Equal(t, childLibs, second.Libraries)

	expression, err := lang.ParseExpr("local", []byte("local.endpoint"))
	require.NoError(t, err)
	for _, scope := range []*EvalContext{first, second} {
		value, err := Eval(expression, scope)
		require.NoError(t, err)
		require.Equal(t, scope.Inputs["endpoint"], value)
	}
	first.Resources["private"] = map[string]any{"id": "A"}
	require.Empty(t, root.Resources)
	require.Empty(t, second.Resources)
}
