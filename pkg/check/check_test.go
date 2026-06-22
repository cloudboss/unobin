package check

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/ubtest"
)

func TestNewSyntaxBuildsDAGFromTypedBody(t *testing.T) {
	src := ubtest.ReadValidFixture(t, "testdata/ub/syntax-dag", "library-config")
	sf, err := syntax.ParseSource("factory.ub", []byte(src))
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
	composite := parseSyntaxCompositeFixture(
		t, ubtest.ReadValidFixture(t, "testdata/ub/syntax-dag", "composite"))
	fixture := parseSyntaxFactoryFixture(
		t, ubtest.ReadValidFixture(t, "testdata/ub/syntax-dag", "composite-call"))
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
	src := ubtest.ReadValidFixture(t, "testdata/ub/syntax-dag", "schemaless-output")
	errs := checkSyntaxReferences(t, src, map[string]*runtime.Library{
		"local": {},
	})

	require.Empty(t, checkRefMessages(t, errs))
}

func TestCheckReferenceFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/references", func(name string, src []byte) (string, []string) {
		f, err := syntax.ParseSource("factory.ub", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		return "", NewSyntax(f.Factory.Body, nil).References(nil).Messages()
	})
}

func checkRefMessages(t *testing.T, errs *lang.ErrorList) []string {
	t.Helper()
	for _, err := range errs.Errors() {
		require.Equal(t, lang.ErrResolve, err.Kind)
	}
	return errs.Messages()
}
