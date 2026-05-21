package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/require"
)

// localFileModule mirrors the input and output fields of the real
// `local.file` resource so the tests don't pull the modules
// package as a dependency.
func localFileModule() *Module {
	return &Module{
		Schema: &ModuleSchema{
			Resources: map[string]*TypeSchema{
				"file": {
					Inputs: map[string]typecheck.Type{
						"path":             typecheck.TString(),
						"content":          typecheck.TString(),
						"mode":             typecheck.TInteger(),
						"create-directory": typecheck.TBoolean(),
					},
					Outputs: map[string]typecheck.Type{
						"path":   typecheck.TString(),
						"sha256": typecheck.TString(),
						"size":   typecheck.TInteger(),
					},
				},
			},
		},
	}
}

func TestCheckTypesAcceptsMatchingBody(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: { path: { type: string } }
resources: {
  local: {
    file: {
      one: { path: var.path, content: 'hi' }
    }
  }
}
`), map[string]*Module{"local": localFileModule()})

	require.Empty(t, checkRefMessages(t, errs))
}

func TestCheckTypesRejectsLiteralIntoStringField(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
resources: {
  local: {
    file: {
      one: { path: 5, content: 'hi' }
    }
  }
}
`), map[string]*Module{"local": localFileModule()})

	got := checkErrorMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesRejectsVarWithWrongType(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: { mode: { type: integer } }
resources: {
  local: {
    file: {
      one: { path: var.mode, content: 'hi' }
    }
  }
}
`), map[string]*Module{"local": localFileModule()})

	got := checkErrorMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesRejectsResourceFieldWithWrongType(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
resources: {
  local: {
    file: {
      one: { path: 'one', content: 'hi' }
      two: { path: resource.local.file.one.size, content: 'hi' }
    }
  }
}
`), map[string]*Module{"local": localFileModule()})

	got := checkErrorMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesAcceptsOptionalIntoRequired(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: { p: { type: optional(string, 'x') } }
resources: {
  local: {
    file: {
      one: { path: var.p, content: 'hi' }
    }
  }
}
`), map[string]*Module{"local": localFileModule()})

	require.Empty(t, checkErrorMessages(t, errs))
}

func TestCheckTypesRejectsListWithWrongElementType(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
actions: {
  core: {
    command: {
      x: { argv: ['echo', 5] }
    }
  }
}
`), map[string]*Module{
		"core": {Schema: &ModuleSchema{
			Actions: map[string]*TypeSchema{
				"command": {
					Inputs: map[string]typecheck.Type{
						"argv": typecheck.TList(typecheck.TString()),
					},
				},
			},
		}},
	})

	got := checkErrorMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesAcceptsListLiteralMatchingTarget(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
actions: {
  core: {
    command: {
      x: { argv: ['echo', 'hi'] }
    }
  }
}
`), map[string]*Module{
		"core": {Schema: &ModuleSchema{
			Actions: map[string]*TypeSchema{
				"command": {
					Inputs: map[string]typecheck.Type{
						"argv": typecheck.TList(typecheck.TString()),
					},
				},
			},
		}},
	})
	require.Empty(t, checkErrorMessages(t, errs))
}

func TestCheckTypesRejectsConstraintWithNonBooleanPredicate(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: { region: { type: string } }
constraints: [
  {
    kind: predicate
    when: var.region
    require: var.region == 'us-east-1'
  }
]
`), nil)

	got := checkErrorMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected boolean, got string")
}

func TestCheckTypesAcceptsForEachOverSet(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: { names: { type: set(string) } }
resources: {
  local: {
    file: {
      many: {
        @for-each: var.names
        path: @each.value
        content: 'hi'
      }
    }
  }
}
`), map[string]*Module{"local": localFileModule()})
	require.Empty(t, checkErrorMessages(t, errs))
}

func TestCheckTypesRejectsForEachValueIntoWrongSlot(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: { counts: { type: map(integer) } }
resources: {
  local: {
    file: {
      many: {
        @for-each: var.counts
        path: @each.value
        content: 'hi'
      }
    }
  }
}
`), map[string]*Module{"local": localFileModule()})

	got := checkErrorMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesRejectsUnknownBodyField(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
resources: {
  local: {
    file: {
      one: { paht: 'x', content: 'hi' }
    }
  }
}
`), map[string]*Module{"local": localFileModule()})

	got := checkErrorMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown field "paht" on local.file`)
}

func TestCheckTypesRejectsUnknownFieldOnNestedResourceOutput(t *testing.T) {
	endpoint := typecheck.TObject([]typecheck.ObjectField{
		{Name: "host", Type: typecheck.TString()},
		{Name: "port", Type: typecheck.TInteger()},
	})
	errs := CheckReferences(parseStack(t, `
resources: {
  aws: {
    rds: {
      main: { name: 'one' }
    }
  }
  local: {
    file: {
      one: {
        path: resource.aws.rds.main.endpoint.bogus
        content: 'hi'
      }
    }
  }
}
`), map[string]*Module{
		"local": localFileModule(),
		"aws": {Schema: &ModuleSchema{
			Resources: map[string]*TypeSchema{
				"rds": {
					Inputs: map[string]typecheck.Type{
						"name": typecheck.TString(),
					},
					Outputs: map[string]typecheck.Type{
						"endpoint": endpoint,
					},
				},
			},
		}},
	})

	got := checkErrorMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown field "bogus" on object(`)
}

func TestCheckTypesRejectsUnknownNestedObjectField(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: {
  cfg: { type: object({ host: string  port: integer }) }
}
resources: {
  local: {
    file: {
      one: { path: var.cfg.bogus, content: 'hi' }
    }
  }
}
`), map[string]*Module{"local": localFileModule()})

	got := checkErrorMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown field "bogus" on object(`)
}

func TestCheckTypesSkipsWhenInputsSchemaAbsent(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
resources: {
  local: {
    file: {
      one: { path: 5, content: 'hi' }
    }
  }
}
`), map[string]*Module{
		"local": {Schema: &ModuleSchema{
			Resources: map[string]*TypeSchema{
				"file": {Outputs: map[string]typecheck.Type{"path": typecheck.TString()}},
			},
		}},
	})
	require.Empty(t, checkErrorMessages(t, errs))
}

// checkErrorMessages returns the messages of every diagnostic
// regardless of kind. Used by the type-check tests because their
// errors come back as ErrType while reference checks produce
// ErrResolve.
func checkErrorMessages(t *testing.T, errs *lang.ErrorList) []string {
	t.Helper()
	require.NotNil(t, errs)
	var out []string
	for _, err := range errs.Errors() {
		out = append(out, err.Msg)
	}
	return out
}
