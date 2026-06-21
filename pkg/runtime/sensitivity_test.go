package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func syntaxSensitivity(
	t *testing.T,
	src string,
	libs map[string]*Library,
) (*DAG, *sensitivityAnalyzer) {
	t.Helper()
	fixture := parseSyntaxFactoryFixture(t, "factory: {\n"+src+"\n}")
	dag := BuildSyntaxDAG(fixture.body, libs)
	return dag, newSensitivityAnalyzerFromSource(nil, &fixture.body, libs, dag)
}

func TestSensitivityLocalNonSensitive(t *testing.T) {
	src := `
inputs:    { region: { type: string } }
locals:    { r: var.region }
resources: { file: local.file { path: local.r } }
`
	libs := map[string]*Library{"local": {Name: "local"}}
	dag, an := syntaxSensitivity(t, src, libs)

	node := dag.Nodes["resource.file"]
	require.NotNil(t, node)
	require.Empty(t, an.sensitiveInputs(node.Body, node.Composite))
}

func TestSensitivityLocalCycleTerminates(t *testing.T) {
	src := `
locals:    { a: local.b, b: local.a }
resources: { file: local.file { path: local.a } }
`
	libs := map[string]*Library{"local": {Name: "local"}}
	dag, an := syntaxSensitivity(t, src, libs)

	node := dag.Nodes["resource.file"]
	require.NotNil(t, node)
	require.Empty(t, an.sensitiveInputs(node.Body, node.Composite))
}

// An object-valued local with one sensitive field must not mask the
// other fields. Reading the non-sensitive field through the local is
// not sensitive; reading the sensitive field is.
func TestSensitivityNarrowsObjectLocalToNavigatedField(t *testing.T) {
	src := `
inputs:    { user: { type: string }, password: { type: string, @sensitive: true } }
locals:    { creds: { name: var.user, secret: var.password } }
resources: { file: local.file { path: local.creds.name, content: local.creds.secret } }
`
	libs := map[string]*Library{"local": {Name: "local"}}
	dag, an := syntaxSensitivity(t, src, libs)

	node := dag.Nodes["resource.file"]
	require.NotNil(t, node)
	require.Equal(t, []string{"content"}, an.sensitiveInputs(node.Body, node.Composite))
}

func TestSensitivityRecognizesSensitiveGoOutput(t *testing.T) {
	src := `
resources: {
  secret: vault.secret { name: 'token' }
  file:   local.file { path: 'out.txt', content: resource.secret.value }
}
`
	libs := map[string]*Library{
		"vault": {
			Name: "vault",
			Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
				"secret": {SensitiveOutputs: []string{"value"}},
			}},
		},
		"local": {Name: "local"},
	}
	dag, an := syntaxSensitivity(t, src, libs)

	node := dag.Nodes["resource.file"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got)
}

func TestSensitivityRecognizesSensitiveShortGoOutput(t *testing.T) {
	src := `
resources: {
  secret: vault.secret { name: 'token' }
  file: local.file { path: 'out.txt', content: resource.secret.value }
}
`
	libs := map[string]*Library{
		"vault": {
			Name: "vault",
			Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
				"secret": {SensitiveOutputs: []string{"value"}},
			}},
		},
		"local": {Name: "local"},
	}
	dag, an := syntaxSensitivity(t, src, libs)

	node := dag.Nodes["resource.file"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got)
}

func TestSensitivityRecognizesSensitiveGoInput(t *testing.T) {
	src := `
resources: {
  secret: vault.secret { token: 'shh' }
  file:   local.file { path: 'out.txt', content: resource.secret.token }
}
`
	libs := map[string]*Library{
		"vault": {
			Name: "vault",
			Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
				"secret": {SensitiveInputs: []string{"token"}},
			}},
		},
		"local": {Name: "local"},
	}
	dag, an := syntaxSensitivity(t, src, libs)

	node := dag.Nodes["resource.file"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got,
		"a reader of a sensitive input is masked, the same as a sensitive output")
}

func TestSensitivityRecognizesNonSensitiveGoOutput(t *testing.T) {
	src := `
resources: {
  secret: vault.secret { name: 'token' }
  file:   local.file { path: 'out.txt', content: resource.secret.arn }
}
`
	libs := map[string]*Library{
		"vault": {
			Name: "vault",
			Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
				"secret": {SensitiveOutputs: []string{"value"}},
			}},
		},
		"local": {Name: "local"},
	}
	dag, an := syntaxSensitivity(t, src, libs)

	node := dag.Nodes["resource.file"]
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Empty(t, got)
}

func TestSensitivityPropagatesCompositeOutputDeclared(t *testing.T) {
	composite := syntaxResourceComposite(t, "box", `
inputs: { message: { type: string } }

resources: { this: local.file { path: 'x.txt', content: var.message } }

outputs: { token: { value: resource.this.sha256, @sensitive: true } }
`)
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
	dag, an := syntaxSensitivity(t, `
resources: {
  one: wrap.box { message: 'hi' }
  f:   local.file { path: 'out.txt', content: resource.one.token }
}
`, libs)

	node := dag.Nodes["resource.f"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got)
}

func TestSensitivityPropagatesTypedCompositeOutputDeclared(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
box: resource {
  outputs: { token: { value: 'secret', @sensitive: true } }
}
`)
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
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  resources: {
    one: wrap.box {}
    file: local.file { path: 'out.txt', content: resource.one.token }
  }
}
`)
	dag := BuildSyntaxDAG(fixture.body, libs)
	an := newSensitivityAnalyzerFromSource(nil, &fixture.body, libs, dag)

	node := dag.Nodes["resource.file"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got)
}

func TestSensitivityInsideTypedCompositeUsesSyntaxInputs(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
box: resource {
  inputs: { password: { type: string, @sensitive: true } }
  resources: { this: local.file { path: 'x.txt', content: var.password } }
}
`)
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
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  resources: { one: wrap.box { password: 'shh' } }
}
`)
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
	composite := syntaxResourceComposite(t, "box", `
resources: { this: vault.secret { name: 'x' } }

outputs: { forwarded: { value: resource.this.value } }
`)
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
	dag, an := syntaxSensitivity(t, `
resources: {
  one: wrap.box {}
  f:   local.file { path: 'out.txt', content: resource.one.forwarded }
}
`, libs)

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
	composite := syntaxResourceComposite(t, "box", `
resources: { this: vault.secret { name: 'x' } }

outputs: { arn: { value: resource.this.arn } }
`)
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
	dag, an := syntaxSensitivity(t, `
resources: {
  one: wrap.box {}
  f:   local.file { path: 'out.txt', content: resource.one.arn }
}
`, libs)

	node := dag.Nodes["resource.f"]
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Empty(t, got)
}

func TestSensitivityInsideCompositeUsesCompositeInputs(t *testing.T) {
	composite := syntaxResourceComposite(t, "box", `
inputs: { password: { type: string, @sensitive: true } }

resources: { this: local.file { path: 'x.txt', content: var.password } }

outputs: { sha: { value: resource.this.sha256 } }
`)
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
	dag, an := syntaxSensitivity(t, `
resources: { one: wrap.box { password: 'shh' } }
`, libs)

	inner := dag.Nodes["resource.one/resource.this"]
	require.NotNil(t, inner, "internal node should exist")
	require.Equal(t, "resource.one", inner.Composite)
	got := an.sensitiveInputs(inner.Body, inner.Composite)
	require.Equal(t, []string{"content"}, got,
		"composite-internal node reading var.password should be flagged sensitive")
}

func TestSensitivityHandlesNilSource(t *testing.T) {
	libs := map[string]*Library{}
	an := newSensitivityAnalyzer(nil, libs, nil)
	body := &lang.ObjectLit{}
	require.Empty(t, an.sensitiveInputs(body, ""))
}

