package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/require"
)

func TestCheckReferencesRootScope(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: {
  path: { type: string }
}
resources: {
  local: {
    file: {
      one: {
        path: var.missing
        content: resource.local.file.absent.content
      }
    }
  }
}
outputs: {
  good: { value: resource.local.file.one.path }
  bad: { value: data.core.lookup.missing.value }
}
`), nil)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 3)
	require.Contains(t, got[0], `unknown input "missing"`)
	require.Contains(t, got[1], `unknown resource "resource.local.file.absent"`)
	require.Contains(t, got[2], `unknown data "data.core.lookup.missing"`)
}

func TestCheckReferencesLocalsValid(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: { env: { type: string } }
locals: {
  base:    var.env
  derived: local.base
}
resources: {
  local: { file: { one: { path: local.derived } } }
}
outputs: {
  p: { value: resource.local.file.one.path }
}
`), nil)
	require.Empty(t, checkRefMessages(t, errs))
}

func TestCheckReferencesUnknownLocal(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
outputs: {
  bad: { value: local.nope }
}
`), nil)
	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown local "nope"`)
}

func TestCheckReferencesFunctionExists(t *testing.T) {
	libs := map[string]*Library{
		"core": {Schema: &LibrarySchema{Functions: map[string]bool{"format": true}}},
	}
	errs := CheckReferences(parseStack(t, `
actions: {
  core: { command: { x: { argv: [core.format('%s', 'hi')] } } }
}
`), libs)
	require.Empty(t, checkRefMessages(t, errs))
}

func TestCheckReferencesUnknownFunction(t *testing.T) {
	libs := map[string]*Library{
		"core": {Schema: &LibrarySchema{Functions: map[string]bool{"format": true}}},
	}
	errs := CheckReferences(parseStack(t, `
actions: {
  core: { command: { x: { argv: [core.formatt('%s', 'hi')] } } }
}
`), libs)
	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `library "core" has no function "formatt"`)
}

func TestCheckReferencesLocalReadsUnknownInput(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
locals: { x: var.missing }
outputs: { o: { value: local.x } }
`), nil)
	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown input "missing"`)
}

func TestCheckReferencesLocalCycle(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
locals: {
  a: local.b
  b: local.a
}
outputs: { o: { value: local.a } }
`), nil)
	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `is part of a cycle`)
}

func TestCheckReferencesResourceModuleMustBeImported(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
resources: {
  greeter: {
    greeting: {
      welcome: {
        message: 'hello'
      }
    }
  }
}
`), map[string]*Library{
		"local": {},
	})

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `library "greeter" is not imported`)
}

func TestCheckReferencesCompositeScope(t *testing.T) {
	composite := parseStack(t, `
inputs: {
  path: { type: string }
}
resources: {
  local: {
    file: {
      one: {
        path: var.path
        content: 'hello'
      }
      two: {
        path: resource.local.file.one.path
        content: 'world'
      }
    }
  }
}
outputs: {
  path: { value: resource.local.file.two.path }
}
`)
	libs := map[string]*Library{
		"bundle": {
			ResourceComposites: map[string]*CompositeType{
				"file-pair": {
					Name:      "file-pair",
					Body:      composite,
					Libraries: map[string]*Library{"local": {}},
				},
			},
		},
	}

	errs := CheckReferences(parseStack(t, `
inputs: {
  target: { type: string }
}
resources: {
  bundle: {
    file-pair: {
      demo: { path: var.target }
    }
  }
}
outputs: {
  path: { value: resource.bundle.file-pair.demo.path }
}
`), libs)

	require.Empty(t, checkRefMessages(t, errs))
}

func TestCheckReferencesCompositeUnknownsUseCompositeScope(t *testing.T) {
	composite := parseStack(t, `
inputs: {
  path: { type: string }
}
resources: {
  local: {
    file: {
      one: {
        path: var.missing
        content: resource.local.file.absent.content
      }
    }
  }
}
outputs: {
  path: { value: resource.local.file.one.path }
}
`)
	libs := map[string]*Library{
		"bundle": {
			ResourceComposites: map[string]*CompositeType{
				"file-pair": {
					Name:      "file-pair",
					Body:      composite,
					Libraries: map[string]*Library{"local": {}},
				},
			},
		},
	}

	errs := CheckReferences(parseStack(t, `
resources: {
  bundle: {
    file-pair: {
      demo: { path: 'x.txt' }
    }
  }
}
`), libs)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 2)
	require.Contains(t, got[0], `unknown input "missing"`)
	require.Contains(t, got[1], `unknown resource "resource.local.file.absent"`)
}

func TestCheckReferencesConstraintPredicateRootScope(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: {
  region: { type: string }
}
constraints: [
  {
    kind: predicate
    when: var.bogus == 'x'
    require: var.also-missing == true
    message: 'should error'
  }
]
`), nil)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 2)
	require.Contains(t, got[0], `unknown input "bogus"`)
	require.Contains(t, got[1], `unknown input "also-missing"`)
}

func TestCheckReferencesConstraintPredicateCompositeScope(t *testing.T) {
	composite := parseStack(t, `
inputs: {
  region: { type: string }
}
constraints: [
  {
    kind: predicate
    when: var.bogus == 'x'
    require: var.region == 'y'
  }
]
resources: {
  local: {
    file: {
      one: { path: var.region content: 'hi' }
    }
  }
}
outputs: {
  path: { value: resource.local.file.one.path }
}
`)
	libs := map[string]*Library{
		"bundle": {
			ResourceComposites: map[string]*CompositeType{
				"thing": {
					Name:      "thing",
					Body:      composite,
					Libraries: map[string]*Library{"local": {}},
				},
			},
		},
	}

	errs := CheckReferences(parseStack(t, `
resources: {
  bundle: {
    thing: {
      demo: { region: 'us-east-1' }
    }
  }
}
`), libs)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown input "bogus"`)
}

func TestCheckReferencesUnknownTrailingField(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: { path: { type: string } }
resources: {
  local: {
    file: {
      one: { path: var.path, content: 'hi' }
    }
  }
}
outputs: {
  ok:  { value: resource.local.file.one.path }
  bad: { value: resource.local.file.one.bogus }
}
`), map[string]*Library{
		"local": {Schema: &LibrarySchema{
			Resources: map[string]*TypeSchema{
				"file": {Outputs: map[string]typecheck.Type{
					"path":   typecheck.TString(),
					"sha256": typecheck.TString(),
					"size":   typecheck.TInteger(),
				}},
			},
		}},
	})

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown field "bogus"`)
	require.Contains(t, got[0], `local.file`)
}

func TestCheckReferencesActionFieldMustExist(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
actions: {
  core: {
    command: {
      x: { argv: ['true'] }
    }
  }
}
outputs: {
  bad: { value: action.core.command.x.nope }
}
`), map[string]*Library{
		"core": {Schema: &LibrarySchema{
			Actions: map[string]*TypeSchema{
				"command": {Outputs: map[string]typecheck.Type{
					"stdout":    typecheck.TString(),
					"exit-code": typecheck.TInteger(),
				}},
			},
		}},
	})

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown field "nope"`)
}

func TestCheckReferencesCompositeOutputMustBeDeclared(t *testing.T) {
	composite := parseStack(t, `
resources: {
  local: {
    file: {
      one: { path: 'x.txt', content: 'hi' }
    }
  }
}
outputs: {
  path: { value: resource.local.file.one.path }
}
`)
	libs := map[string]*Library{
		"bundle": {
			ResourceComposites: map[string]*CompositeType{
				"thing": {
					Name: "thing",
					Body: composite,
					Libraries: map[string]*Library{"local": {
						Schema: &LibrarySchema{
							Resources: map[string]*TypeSchema{
								"file": {
									Outputs: map[string]typecheck.Type{
										"path": typecheck.TString(),
									},
								},
							},
						},
					}},
				},
			},
		},
	}

	errs := CheckReferences(parseStack(t, `
resources: {
  bundle: {
    thing: {
      demo: {}
    }
  }
}
outputs: {
  ok:  { value: resource.bundle.thing.demo.path }
  bad: { value: resource.bundle.thing.demo.bogus }
}
`), libs)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown field "bogus"`)
}

func TestCheckReferencesDataSourceFieldMustExist(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
data: {
  aws: {
    ami: {
      ubuntu: { most-recent: true }
    }
  }
}
outputs: {
  ok:  { value: data.aws.ami.ubuntu.id }
  bad: { value: data.aws.ami.ubuntu.misspelled }
}
`), map[string]*Library{
		"aws": {Schema: &LibrarySchema{
			DataSources: map[string]*TypeSchema{
				"ami": {Outputs: map[string]typecheck.Type{
					"id":           typecheck.TString(),
					"architecture": typecheck.TString(),
				}},
			},
		}},
	})

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown field "misspelled"`)
	require.Contains(t, got[0], `aws.ami`)
}

func TestCheckReferencesForEachInstanceFieldMustExist(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: {
  names: { type: set(string) }
}
resources: {
  local: {
    file: {
      many: {
        @for-each: var.names
        path:      @each.value
        content:   'hello'
      }
    }
  }
}
outputs: {
  ok:  { value: resource.local.file.many['greet'].path }
  bad: { value: resource.local.file.many['greet'].whatever }
}
`), map[string]*Library{
		"local": {Schema: &LibrarySchema{
			Resources: map[string]*TypeSchema{
				"file": {Outputs: map[string]typecheck.Type{
					"path":   typecheck.TString(),
					"sha256": typecheck.TString(),
				}},
			},
		}},
	})

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown field "whatever"`)
}

func TestCheckReferencesSkipsFieldCheckWhenNoSchema(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
resources: {
  local: {
    file: {
      one: { path: 'x.txt' }
    }
  }
}
outputs: {
  anything: { value: resource.local.file.one.whatever }
}
`), map[string]*Library{
		"local": {},
	})

	require.Empty(t, checkRefMessages(t, errs))
}

func TestCheckReferencesEachScope(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: {
  files: { type: list(string) }
}
resources: {
  local: {
    file: {
      many: {
        @for-each: var.files
        path: @each.key
        content: @each.value
      }
      mirror: {
        @for-each: var.files
        path: resource.local.file.many[@each.key].path
        content: @each.value
      }
      bad: {
        path: @each.key
        content: 'no loop'
      }
    }
  }
}
`), nil)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `@each is only available inside @for-each`)
}

func checkRefMessages(t *testing.T, errs *lang.ErrorList) []string {
	t.Helper()
	require.NotNil(t, errs)
	var out []string
	for _, err := range errs.Errors() {
		require.Equal(t, lang.ErrResolve, err.Kind)
		out = append(out, err.Msg)
	}
	return out
}
