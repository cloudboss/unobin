package check

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func TestNewSyntaxBuildsDAGFromTypedBody(t *testing.T) {
	sf, err := syntax.ParseSource("factory.ub", []byte(`
factory: {
  library-configs: { k8s: { region: resource.cluster.endpoint } }
  resources: {
    cluster: aws.eks { name: 'web' }
    apps: k8s.namespace { name: 'apps' }
  }
}
`))
	require.NoError(t, err)
	require.NotNil(t, sf.Factory)
	k8s := libraryConfigSchemaLibrary("")
	k8s.Schema.Resources = map[string]*runtime.TypeSchema{"namespace": {}}
	checker := NewSyntax(sf.Factory.Body, map[string]*runtime.Library{
		"aws": {},
		"k8s": k8s,
	})
	dag := checker.DAG()

	require.Contains(t, dag.Nodes, "resource.apps")
	require.Contains(t, dag.Edges["resource.apps"], "library-config.k8s")
	require.Empty(t, checkRefMessages(t, checker.References(nil)))
}

func TestNewSyntaxUsesCompositeSyntaxScope(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
greeting: resource {
  inputs: { path: { type: string } }
  locals: { target: var.path }
  resources: {
    file: local.fs-file { path: local.target }
  }
}
`)
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  resources: {
    app: outer.greeting { path: '/tmp/app' }
  }
}
`)
	body := composite.body
	checker := NewSyntax(fixture.body, map[string]*runtime.Library{
		"outer": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"greeting": {
					Name:       "greeting",
					SyntaxBody: &body,
					Libraries:  map[string]*runtime.Library{"local": {}},
				},
			},
		},
	})

	require.Empty(t, checkRefMessages(t, checker.References(nil)))
}

func TestCheckReferencesSkipsFieldCheckWhenNoSchema(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: { one: local.file { path: 'x.txt' } }
outputs:   { anything: { value: resource.one.whatever } }
`, map[string]*runtime.Library{
		"local": {},
	})

	require.Empty(t, checkRefMessages(t, errs))
}

func checkRefMessages(t *testing.T, errs *lang.ErrorList) []string {
	t.Helper()
	for _, err := range errs.Errors() {
		require.Equal(t, lang.ErrResolve, err.Kind)
	}
	return errs.Messages()
}
