package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/state"
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
	libs := map[string]*Library{"local": {Name: "local"}}
	dag := BuildDAG(f, libs)
	an := newSensitivityAnalyzer(f, libs, dag)

	node := dag.Nodes["resource.local.secret.one"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"password"}, got)
}

func TestSensitivityFollowsLocalToSensitiveVar(t *testing.T) {
	src := `
inputs: {
  region:   { type: string }
  password: { type: string  @sensitive: true }
}
locals: {
  pw: var.password
}
resources: {
  local: {
    secret: {
      one: {
        name:     var.region
        password: local.pw
      }
    }
  }
}
`
	f := parseStack(t, src)
	libs := map[string]*Library{"local": {Name: "local"}}
	dag := BuildDAG(f, libs)
	an := newSensitivityAnalyzer(f, libs, dag)

	node := dag.Nodes["resource.local.secret.one"]
	require.NotNil(t, node)
	require.Equal(t, []string{"password"}, an.sensitiveInputs(node.Body, node.Composite))
}

func TestSensitivityFollowsChainedLocal(t *testing.T) {
	src := `
inputs: {
  plain:  { type: string }
  secret: { type: string  @sensitive: true }
}
locals: {
  a: var.secret
  b: local.a
  c: var.plain
}
resources: {
  local: {
    secret: {
      one: {
        name:     local.c
        password: local.b
      }
    }
  }
}
`
	f := parseStack(t, src)
	libs := map[string]*Library{"local": {Name: "local"}}
	dag := BuildDAG(f, libs)
	an := newSensitivityAnalyzer(f, libs, dag)

	node := dag.Nodes["resource.local.secret.one"]
	require.NotNil(t, node)
	require.Equal(t, []string{"password"}, an.sensitiveInputs(node.Body, node.Composite))
}

func TestSensitivityFollowsLocalToSensitiveOutput(t *testing.T) {
	src := `
locals: {
  tok: resource.vault.secret.s.value
}
resources: {
  vault: { secret: { s: { name: 'token' } } }
  local: {
    file: {
      f: {
        path:    'out.txt'
        content: local.tok
      }
    }
  }
}
`
	f := parseStack(t, src)
	libs := map[string]*Library{
		"vault": {
			Name: "vault",
			Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
				"secret": {SensitiveOutputs: []string{"value"}},
			}},
		},
		"local": {Name: "local"},
	}
	dag := BuildDAG(f, libs)
	an := newSensitivityAnalyzer(f, libs, dag)

	node := dag.Nodes["resource.local.file.f"]
	require.NotNil(t, node)
	require.Equal(t, []string{"content"}, an.sensitiveInputs(node.Body, node.Composite))
}

func TestSensitivityLocalNonSensitive(t *testing.T) {
	src := `
inputs: { region: { type: string } }
locals: { r: var.region }
resources: {
  local: { file: { f: { path: local.r } } }
}
`
	f := parseStack(t, src)
	libs := map[string]*Library{"local": {Name: "local"}}
	dag := BuildDAG(f, libs)
	an := newSensitivityAnalyzer(f, libs, dag)

	node := dag.Nodes["resource.local.file.f"]
	require.NotNil(t, node)
	require.Empty(t, an.sensitiveInputs(node.Body, node.Composite))
}

func TestSensitivityLocalCycleTerminates(t *testing.T) {
	src := `
locals: {
  a: local.b
  b: local.a
}
resources: {
  local: { file: { f: { path: local.a } } }
}
`
	f := parseStack(t, src)
	libs := map[string]*Library{"local": {Name: "local"}}
	dag := BuildDAG(f, libs)
	an := newSensitivityAnalyzer(f, libs, dag)

	node := dag.Nodes["resource.local.file.f"]
	require.NotNil(t, node)
	require.Empty(t, an.sensitiveInputs(node.Body, node.Composite))
}

// An object-valued local with one sensitive field must not mask the
// other fields. Reading the non-sensitive field through the local is
// not sensitive; reading the sensitive field is.
func TestSensitivityNarrowsObjectLocalToNavigatedField(t *testing.T) {
	src := `
inputs: {
  user:     { type: string }
  password: { type: string  @sensitive: true }
}
locals: {
  creds: {
    name:   var.user
    secret: var.password
  }
}
resources: {
  local: {
    file: {
      f: {
        path:    local.creds.name
        content: local.creds.secret
      }
    }
  }
}
`
	f := parseStack(t, src)
	libs := map[string]*Library{"local": {Name: "local"}}
	dag := BuildDAG(f, libs)
	an := newSensitivityAnalyzer(f, libs, dag)

	node := dag.Nodes["resource.local.file.f"]
	require.NotNil(t, node)
	require.Equal(t, []string{"content"}, an.sensitiveInputs(node.Body, node.Composite))
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
	libs := map[string]*Library{
		"vault": {
			Name: "vault",
			Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
				"secret": {SensitiveOutputs: []string{"value"}},
			}},
		},
		"local": {Name: "local"},
	}
	dag := BuildDAG(f, libs)
	an := newSensitivityAnalyzer(f, libs, dag)

	node := dag.Nodes["resource.local.file.f"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got)
}

func TestSensitivityRecognizesSensitiveGoInput(t *testing.T) {
	src := `
resources: {
  vault: {
    secret: {
      s: { token: 'shh' }
    }
  }
  local: {
    file: {
      f: {
        path: 'out.txt'
        content: resource.vault.secret.s.token
      }
    }
  }
}
`
	f := parseStack(t, src)
	libs := map[string]*Library{
		"vault": {
			Name: "vault",
			Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
				"secret": {SensitiveInputs: []string{"token"}},
			}},
		},
		"local": {Name: "local"},
	}
	dag := BuildDAG(f, libs)
	an := newSensitivityAnalyzer(f, libs, dag)

	node := dag.Nodes["resource.local.file.f"]
	require.NotNil(t, node)
	got := an.sensitiveInputs(node.Body, node.Composite)
	require.Equal(t, []string{"content"}, got,
		"a reader of a sensitive input is masked, the same as a sensitive output")
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
	libs := map[string]*Library{
		"vault": {
			Name: "vault",
			Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
				"secret": {SensitiveOutputs: []string{"value"}},
			}},
		},
		"local": {Name: "local"},
	}
	dag := BuildDAG(f, libs)
	an := newSensitivityAnalyzer(f, libs, dag)

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
	libs := map[string]*Library{
		"wrap": {
			Name: "wrap",
			ResourceComposites: map[string]*CompositeType{
				"box": {Name: "box", Body: composite, Libraries: map[string]*Library{
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
	dag := BuildDAG(stack, libs)
	an := newSensitivityAnalyzer(stack, libs, dag)

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
	libs := map[string]*Library{
		"wrap": {
			Name: "wrap",
			ResourceComposites: map[string]*CompositeType{
				"box": {Name: "box", Body: composite, Libraries: map[string]*Library{
					"vault": {
						Name: "vault",
						Schema: &LibrarySchema{
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
	dag := BuildDAG(stack, libs)
	an := newSensitivityAnalyzer(stack, libs, dag)

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
	libs := map[string]*Library{
		"wrap": {
			Name: "wrap",
			ResourceComposites: map[string]*CompositeType{
				"box": {Name: "box", Body: composite, Libraries: map[string]*Library{
					"vault": {
						Name: "vault",
						Schema: &LibrarySchema{
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
	dag := BuildDAG(stack, libs)
	an := newSensitivityAnalyzer(stack, libs, dag)

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
	libs := map[string]*Library{
		"wrap": {
			Name: "wrap",
			ResourceComposites: map[string]*CompositeType{
				"box": {Name: "box", Body: composite, Libraries: map[string]*Library{
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
	dag := BuildDAG(stack, libs)
	an := newSensitivityAnalyzer(stack, libs, dag)

	inner := dag.Nodes["resource.wrap.box.one/resource.local.file.this"]
	require.NotNil(t, inner, "internal node should exist")
	require.Equal(t, "resource.wrap.box.one", inner.Composite)
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

func TestSensitivityPersistsOntoStateEntry(t *testing.T) {
	src := `
inputs: {
  message: {
    type: string
    @sensitive: true
  }
}

actions: {
  core: {
    echo: {
      hi: { echo: var.message }
    }
  }
}
`
	libs := testModules()
	libs["core"].Schema = &LibrarySchema{
		Actions: map[string]*TypeSchema{
			"echo": {SensitiveOutputs: []string{"echo"}},
		},
	}
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	f := parseStack(t, src)
	store := newStateStore(t)
	exec := &Executor{
		Source:    f,
		DAG:       BuildDAG(f, libs),
		Libraries: libs,
		Inputs:    map[string]any{"message": "shh"},
		Store:     store,
		Factory:   stack,
	}
	applyOnce(t, exec)

	snap, err := store.Current()
	require.NoError(t, err)

	ent := snap.Find("action.core.echo.hi")
	require.NotNil(t, ent, "echo action should have a state entry")
	require.Equal(t, []string{"echo"}, ent.SensitiveInputs)
	require.Equal(t, []string{"echo"}, ent.SensitiveOutputs)
}
