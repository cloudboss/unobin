package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func sensitivityFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/sensitivity", name)
}

func factorySource(src string) string {
	return "factory" + ": {\n" + src + "\n}"
}

func syntaxSensitivity(
	t *testing.T,
	src string,
	libs map[string]*Library,
) (*DAG, *sensitivityAnalyzer) {
	t.Helper()
	fixture := parseSyntaxFactoryFixture(t, factorySource(src))
	dag := BuildSyntaxDAG(fixture.body, libs)
	return dag, newSensitivityAnalyzerFromSource(nil, &fixture.body, libs, dag)
}

func TestSensitivityLocalNonSensitive(t *testing.T) {
	libs := map[string]*Library{"local": {Name: "local"}}
	dag, an := syntaxSensitivity(t, sensitivityFixture(t, "local-nonsensitive"), libs)

	node := dag.Nodes["resource.file"]
	require.NotNil(t, node)
	require.Empty(t, an.sensitiveInputs(node.Body, node.Composite))
}

func TestSensitivityLocalCycleTerminates(t *testing.T) {
	libs := map[string]*Library{"local": {Name: "local"}}
	dag, an := syntaxSensitivity(t, sensitivityFixture(t, "local-cycle"), libs)

	node := dag.Nodes["resource.file"]
	require.NotNil(t, node)
	require.Empty(t, an.sensitiveInputs(node.Body, node.Composite))
}

// An object-valued local with one sensitive field must not mask the
// other fields. Reading the non-sensitive field through the local is
// not sensitive; reading the sensitive field is.
func TestSensitivityNarrowsObjectLocalToNavigatedField(t *testing.T) {
	libs := map[string]*Library{"local": {Name: "local"}}
	dag, an := syntaxSensitivity(t, sensitivityFixture(t, "object-local"), libs)

	node := dag.Nodes["resource.file"]
	require.NotNil(t, node)
	require.Equal(t, []string{"content"}, an.sensitiveInputs(node.Body, node.Composite))
}

func TestSensitivityRecognizesSensitiveGoInput(t *testing.T) {
	libs := map[string]*Library{
		"vault": {
			Name: "vault",
			Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
				"secret": {SensitiveInputs: []string{"token"}},
			}},
		},
		"local": {Name: "local"},
	}
	dag, an := syntaxSensitivity(t, sensitivityFixture(t, "go-sensitive-input"), libs)

	node := dag.Nodes["resource.file"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got,
		"a reader of a sensitive input is masked, the same as a sensitive output")
}

func TestSensitivityRecognizesNonSensitiveGoOutput(t *testing.T) {
	libs := map[string]*Library{
		"vault": {
			Name: "vault",
			Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
				"secret": {SensitiveOutputs: []string{"value"}},
			}},
		},
		"local": {Name: "local"},
	}
	dag, an := syntaxSensitivity(t, sensitivityFixture(t, "go-nonsensitive-output"), libs)

	node := dag.Nodes["resource.file"]
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Empty(t, got)
}

func TestSensitivityPropagatesCompositeOutputDeclared(t *testing.T) {
	composite := syntaxResourceComposite(t, "box", sensitivityFixture(t, "composite-output-box"))
	composite.Libraries = map[string]*Library{"local": {Name: "local"}}
	libs := map[string]*Library{
		"wrap": {
			Name: "wrap",
			ResourceComposites: map[string]*CompositeType{
				"box": composite,
			},
		},
		"local": {Name: "local"},
	}
	dag, an := syntaxSensitivity(t, sensitivityFixture(t, "composite-output-call"), libs)

	node := dag.Nodes["resource.f"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got)
}

func TestSensitivityPropagatesTypedCompositeOutputDeclared(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, sensitivityFixture(t, "typed-composite-output"))
	body := composite.body
	libs := map[string]*Library{
		"wrap": {
			Name: "wrap",
			ResourceComposites: map[string]*CompositeType{
				"box": {
					Name:       "box",
					SyntaxBody: &body,
				},
			},
		},
		"local": {Name: "local"},
	}
	fixture := parseSyntaxFactoryFixture(t,
		factorySource(sensitivityFixture(t, "typed-composite-output-call")))
	dag := BuildSyntaxDAG(fixture.body, libs)
	an := newSensitivityAnalyzerFromSource(nil, &fixture.body, libs, dag)

	node := dag.Nodes["resource.file"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got)
}

func TestSensitivityInsideTypedCompositeUsesSyntaxInputs(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, sensitivityFixture(t, "typed-composite-input"))
	body := composite.body
	libs := map[string]*Library{
		"wrap": {
			Name: "wrap",
			ResourceComposites: map[string]*CompositeType{
				"box": {
					Name:       "box",
					SyntaxBody: &body,
					Libraries:  map[string]*Library{"local": {Name: "local"}},
				},
			},
		},
		"local": {Name: "local"},
	}
	fixture := parseSyntaxFactoryFixture(t,
		factorySource(sensitivityFixture(t, "typed-composite-input-call")))
	dag := BuildSyntaxDAG(fixture.body, libs)
	an := newSensitivityAnalyzerFromSource(nil, &fixture.body, libs, dag)

	inner := dag.Nodes["resource.one/resource.this"]
	require.NotNil(t, inner)
	got := an.sensitiveInputs(inner.Body, inner.Composite)
	require.Equal(t, []string{"content"}, got)
}

func TestSensitivityPropagatesThroughCompositeOutputFromGoField(t *testing.T) {
	vault := &Library{
		Name: "vault",
		Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
			"secret": {SensitiveOutputs: []string{"value"}},
		}},
	}
	composite := syntaxResourceComposite(t, "box", sensitivityFixture(t, "composite-go-field-box"))
	composite.Libraries = map[string]*Library{"vault": vault}
	libs := map[string]*Library{
		"wrap": {
			Name: "wrap",
			ResourceComposites: map[string]*CompositeType{
				"box": composite,
			},
		},
		"local": {Name: "local"},
	}
	dag, an := syntaxSensitivity(t, sensitivityFixture(t, "composite-go-field-call"), libs)

	node := dag.Nodes["resource.f"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got,
		"composite output should inherit sensitivity from referenced Go field")
}

func TestSensitivityNoFalsePositiveOnPlainComposite(t *testing.T) {
	vault := &Library{
		Name: "vault",
		Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
			"secret": {SensitiveOutputs: []string{"value"}},
		}},
	}
	composite := syntaxResourceComposite(t, "box", sensitivityFixture(t, "composite-plain-box"))
	composite.Libraries = map[string]*Library{"vault": vault}
	libs := map[string]*Library{
		"wrap": {
			Name: "wrap",
			ResourceComposites: map[string]*CompositeType{
				"box": composite,
			},
		},
		"local": {Name: "local"},
	}
	dag, an := syntaxSensitivity(t, sensitivityFixture(t, "composite-plain-call"), libs)

	node := dag.Nodes["resource.f"]
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Empty(t, got)
}

func TestSensitivityInsideCompositeUsesCompositeInputs(t *testing.T) {
	composite := syntaxResourceComposite(t, "box", sensitivityFixture(t, "composite-input-box"))
	composite.Libraries = map[string]*Library{"local": {Name: "local"}}
	libs := map[string]*Library{
		"wrap": {
			Name: "wrap",
			ResourceComposites: map[string]*CompositeType{
				"box": composite,
			},
		},
		"local": {Name: "local"},
	}
	dag, an := syntaxSensitivity(t, sensitivityFixture(t, "composite-input-call"), libs)

	inner := dag.Nodes["resource.one/resource.this"]
	require.NotNil(t, inner, "internal node should exist")
	require.Equal(t, "resource.one", inner.Composite)
	got := an.sensitiveInputs(inner.Body, inner.Composite)
	require.Equal(t, []string{"content"}, got,
		"composite-internal node reading input.password should be flagged sensitive")
}

func TestSensitivityHandlesNilSource(t *testing.T) {
	libs := map[string]*Library{}
	an := newSensitivityAnalyzer(nil, libs, nil)
	body := &lang.ObjectLit{}
	require.Empty(t, an.sensitiveInputs(body, ""))
}
