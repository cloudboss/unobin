package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func TestSensitivityRecognizesSensitiveVar(t *testing.T) {
	src := `
inputs: {
  region: { type: string }
  password: {
    type: string
    @sensitive: true
  }
}

resources: {
  local: {
    secret: {
      one: {
        name: var.region
        password: var.password
      }
    }
  }
}
`
	f := parseStack(t, src)
	mods := map[string]*Module{"local": {Name: "local"}}
	dag := BuildDAG(f, mods)
	an := newSensitivityAnalyzer(f, mods, dag)

	node := dag.Nodes["resource.local.secret.one"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"password"}, got)
}

func TestSensitivityRecognizesSensitiveGoOutput(t *testing.T) {
	src := `
resources: {
  vault: {
    secret: {
      s: { name: 'token' }
    }
  }
  local: {
    file: {
      f: {
        path: 'out.txt'
        content: resource.vault.secret.s.value
      }
    }
  }
}
`
	f := parseStack(t, src)
	mods := map[string]*Module{
		"vault": {
			Name: "vault",
			Schema: &ModuleSchema{Resources: map[string]*TypeSchema{
				"secret": {SensitiveOutputs: []string{"value"}},
			}},
		},
		"local": {Name: "local"},
	}
	dag := BuildDAG(f, mods)
	an := newSensitivityAnalyzer(f, mods, dag)

	node := dag.Nodes["resource.local.file.f"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got)
}

func TestSensitivityRecognizesNonSensitiveGoOutput(t *testing.T) {
	src := `
resources: {
  vault: {
    secret: { s: { name: 'token' } }
  }
  local: {
    file: {
      f: {
        path: 'out.txt'
        content: resource.vault.secret.s.arn
      }
    }
  }
}
`
	f := parseStack(t, src)
	mods := map[string]*Module{
		"vault": {
			Name: "vault",
			Schema: &ModuleSchema{Resources: map[string]*TypeSchema{
				"secret": {SensitiveOutputs: []string{"value"}},
			}},
		},
		"local": {Name: "local"},
	}
	dag := BuildDAG(f, mods)
	an := newSensitivityAnalyzer(f, mods, dag)

	node := dag.Nodes["resource.local.file.f"]
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Empty(t, got)
}

func TestSensitivityPropagatesCompositeOutputDeclared(t *testing.T) {
	composite := parseStack(t, `
inputs: {
  message: { type: string }
}

resources: {
  local: { file: { this: { path: 'x.txt' content: var.message } } }
}

outputs: {
  token: {
    value: resource.local.file.this.sha256
    @sensitive: true
  }
}
`)
	mods := map[string]*Module{
		"wrap": {
			Name: "wrap",
			Composites: map[string]*CompositeType{
				"box": {Name: "box", Body: composite, Modules: map[string]*Module{
					"local": {Name: "local"},
				}},
			},
		},
		"local": {Name: "local"},
	}
	stack := parseStack(t, `
resources: {
  wrap: { box: { one: { message: 'hi' } } }
  local: { file: {
    f: {
      path: 'out.txt'
      content: resource.wrap.box.one.token
    }
  } }
}
`)
	dag := BuildDAG(stack, mods)
	an := newSensitivityAnalyzer(stack, mods, dag)

	node := dag.Nodes["resource.local.file.f"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got)
}

func TestSensitivityPropagatesThroughCompositeOutputFromGoField(t *testing.T) {
	composite := parseStack(t, `
resources: {
  vault: { secret: { this: { name: 'x' } } }
}

outputs: {
  forwarded: {
    value: resource.vault.secret.this.value
  }
}
`)
	mods := map[string]*Module{
		"wrap": {
			Name: "wrap",
			Composites: map[string]*CompositeType{
				"box": {Name: "box", Body: composite, Modules: map[string]*Module{
					"vault": {
						Name: "vault",
						Schema: &ModuleSchema{
							Resources: map[string]*TypeSchema{
								"secret": {
									SensitiveOutputs: []string{
										"value",
									},
								},
							},
						},
					},
				}},
			},
		},
		"local": {Name: "local"},
	}
	stack := parseStack(t, `
resources: {
  wrap: { box: { one: {} } }
  local: { file: {
    f: {
      path: 'out.txt'
      content: resource.wrap.box.one.forwarded
    }
  } }
}
`)
	dag := BuildDAG(stack, mods)
	an := newSensitivityAnalyzer(stack, mods, dag)

	node := dag.Nodes["resource.local.file.f"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got,
		"composite output should inherit sensitivity from referenced Go field")
}

func TestSensitivityNoFalsePositiveOnPlainComposite(t *testing.T) {
	composite := parseStack(t, `
resources: {
  vault: { secret: { this: { name: 'x' } } }
}

outputs: {
  arn: { value: resource.vault.secret.this.arn }
}
`)
	mods := map[string]*Module{
		"wrap": {
			Name: "wrap",
			Composites: map[string]*CompositeType{
				"box": {Name: "box", Body: composite, Modules: map[string]*Module{
					"vault": {
						Name: "vault",
						Schema: &ModuleSchema{
							Resources: map[string]*TypeSchema{
								"secret": {
									SensitiveOutputs: []string{
										"value",
									},
								},
							},
						},
					},
				}},
			},
		},
		"local": {Name: "local"},
	}
	stack := parseStack(t, `
resources: {
  wrap: { box: { one: {} } }
  local: { file: {
    f: {
      path: 'out.txt'
      content: resource.wrap.box.one.arn
    }
  } }
}
`)
	dag := BuildDAG(stack, mods)
	an := newSensitivityAnalyzer(stack, mods, dag)

	node := dag.Nodes["resource.local.file.f"]
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Empty(t, got)
}

func TestSensitivityInsideCompositeUsesCompositeInputs(t *testing.T) {
	composite := parseStack(t, `
inputs: {
  password: {
    type: string
    @sensitive: true
  }
}

resources: {
  local: { file: { this: { path: 'x.txt' content: var.password } } }
}

outputs: {
  sha: { value: resource.local.file.this.sha256 }
}
`)
	mods := map[string]*Module{
		"wrap": {
			Name: "wrap",
			Composites: map[string]*CompositeType{
				"box": {Name: "box", Body: composite, Modules: map[string]*Module{
					"local": {Name: "local"},
				}},
			},
		},
		"local": {Name: "local"},
	}
	stack := parseStack(t, `
resources: {
  wrap: { box: { one: { password: 'shh' } } }
}
`)
	dag := BuildDAG(stack, mods)
	an := newSensitivityAnalyzer(stack, mods, dag)

	inner := dag.Nodes["resource.wrap.box.one/local.file.this"]
	require.NotNil(t, inner, "internal node should exist")
	require.Equal(t, "resource.wrap.box.one", inner.Composite)
	got := an.sensitiveInputs(inner.Body, inner.Composite)
	require.Equal(t, []string{"content"}, got,
		"composite-internal node reading var.password should be flagged sensitive")
}

func TestSensitivityHandlesNilSource(t *testing.T) {
	mods := map[string]*Module{}
	an := newSensitivityAnalyzer(nil, mods, nil)
	body := &lang.ObjectLit{}
	require.Empty(t, an.sensitiveInputs(body, ""))
}
