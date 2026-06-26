package check

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestCheckerScopesInOrderIncludesRootAndCompositeBodies(t *testing.T) {
	checker, compositeBody := scopeIndexChecker(t, "one-call")

	scopes := checker.scopesInOrder()
	require.Len(t, scopes, 2)
	assert.Equal(t, []string{"", "resource.app"}, scopeAddresses(scopes))
	require.Same(t, checker.rootSyntax, scopes[0].body)
	require.Same(t, compositeBody, scopes[1].body)
	assert.Equal(t, []string{"resource.app"}, scopeNodeAddresses(scopes[0]))
	assert.Equal(t, []string{"resource.app/resource.file"}, scopeNodeAddresses(scopes[1]))
	require.Same(t, checker.DAG().Nodes["resource.app"], scopes[0].nodes[0])
	require.Same(t, checker.DAG().Nodes["resource.app/resource.file"], scopes[1].nodes[0])
}

func TestCheckerScopesDeduplicateRepeatedCompositeBody(t *testing.T) {
	checker, compositeBody := scopeIndexChecker(t, "two-calls")

	scopes := checker.scopesInOrder()
	require.Len(t, scopes, 3)
	assert.Equal(t, []string{"", "resource.api", "resource.app"}, scopeAddresses(scopes))
	assert.Equal(t, []string{"resource.api", "resource.app"}, scopesForBody(scopes, compositeBody))
	assert.Equal(t, []string{"resource.api", "resource.app"}, scopeNodeAddresses(scopes[0]))
	assert.Equal(t, []string{"resource.api/resource.file"}, scopeNodeAddresses(scopes[1]))
	assert.Equal(t, []string{"resource.app/resource.file"}, scopeNodeAddresses(scopes[2]))

	bodyScopes := checker.bodyScopesInOrder()
	require.Len(t, bodyScopes, 2)
	assert.Equal(t, []string{"", "resource.api"}, scopeAddresses(bodyScopes))
	require.Same(t, compositeBody, bodyScopes[1].body)
}

func scopeIndexChecker(t *testing.T, rootFixture string) (*Checker, *syntax.FactoryBody) {
	t.Helper()
	composite := parseSyntaxCompositeFixture(
		t, ubtest.ReadValidFixture(t, "testdata/ub/scope-index", "composite"))
	root := parseSyntaxFactoryFixture(
		t, ubtest.ReadValidFixture(t, "testdata/ub/scope-index", rootFixture))
	body := composite.body
	compositeType := &runtime.CompositeType{
		Name:       "greeting",
		SyntaxBody: &body,
		Libraries:  map[string]*runtime.Library{"local": {}},
	}
	checker := NewSyntax(root.body, map[string]*runtime.Library{
		"outer": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"greeting": compositeType,
			},
		},
	})
	return checker, compositeType.SyntaxBody
}

func scopeAddresses(scopes []checkerScope) []string {
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, scope.address)
	}
	return out
}

func scopeNodeAddresses(scope checkerScope) []string {
	out := make([]string, 0, len(scope.nodes))
	for _, node := range scope.nodes {
		out = append(out, node.Address)
	}
	return out
}

func scopesForBody(scopes []checkerScope, body *syntax.FactoryBody) []string {
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if scope.body == body {
			out = append(out, scope.address)
		}
	}
	return out
}
