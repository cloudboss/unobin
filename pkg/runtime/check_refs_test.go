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

func TestCheckReferencesSplat(t *testing.T) {
	const bare = "splat [*] must be followed by a field, like list[*].id"
	cases := []struct {
		name  string
		stack string
		want  []string
	}{
		{
			name: "bare splat on a var",
			stack: "inputs: { things: { type: list(string) } }\n" +
				"outputs: { bad: { value: var.things[*] } }\n",
			want: []string{bare},
		},
		{
			name: "bare splat on a deep var field",
			stack: "inputs: { net: { type: object({ subnets: list(string) }) } }\n" +
				"outputs: { bad: { value: var.net.subnets[*] } }\n",
			want: []string{bare},
		},
		{
			name: "bare splat on a resource output",
			stack: "inputs: { p: { type: string } }\n" +
				"resources: { local: { file: { one: { path: var.p } } } }\n" +
				"outputs: { bad: { value: resource.local.file.one.path[*] } }\n",
			want: []string{bare},
		},
		{
			name: "splat with a field is fine",
			stack: "inputs: { subnets: { type: list(object({ id: string })) } }\n" +
				"outputs: { ids: { value: var.subnets[*].id } }\n",
			want: nil,
		},
		{
			name: "splat followed by an index is fine",
			stack: "inputs: { matrix: { type: list(list(string)) } }\n" +
				"outputs: { first: { value: var.matrix[*][0] } }\n",
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := CheckReferences(parseStack(t, c.stack), nil)
			require.Equal(t, c.want, checkRefMessages(t, errs))
		})
	}
}

// fixedSig builds an untyped signature taking exactly n arguments, the
// view a FunctionType literal registration produces.
func fixedSig(n int) typecheck.FuncSig {
	sig := typecheck.FuncSig{Result: typecheck.TUnknown()}
	for range n {
		sig.Params = append(sig.Params, typecheck.TUnknown())
	}
	return sig
}

// variadicSig builds an untyped signature taking n or more arguments.
func variadicSig(n int) typecheck.FuncSig {
	sig := fixedSig(n)
	unknown := typecheck.TUnknown()
	sig.Variadic = &unknown
	return sig
}

func TestCheckReferencesFunctionExists(t *testing.T) {
	libs := map[string]*Library{
		"core": {Schema: &LibrarySchema{Functions: map[string]typecheck.FuncSig{
			"format": variadicSig(1),
		}}},
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
		"core": {Schema: &LibrarySchema{Functions: map[string]typecheck.FuncSig{
			"format": variadicSig(1),
		}}},
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

// TestCheckReferencesCoreNamespace proves the @core namespace checks at
// compile with no import at all: the set is fixed, so an unknown name
// or a wrong argument count is always an error.
func TestCheckReferencesCoreNamespace(t *testing.T) {
	libs := map[string]*Library{"ext": {Schema: &LibrarySchema{
		Actions: map[string]*TypeSchema{"thing": {}},
	}}}
	cases := []struct {
		name string
		call string
		want string
	}{
		{"known function", "@core.join(['hi'], ',')", ""},
		{"unknown function", "@core.frobnicate('x')", `@core has no function "frobnicate"`},
		{"fixed arity violation", "@core.length('a', 'b')", "@core.length takes 1 argument, got 2"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			errs := CheckReferences(parseStack(t, `
actions: {
  ext: { thing: { x: { argv: [`+tt.call+`] } } }
}
`), libs)
			got := checkRefMessages(t, errs)
			if tt.want == "" {
				require.Empty(t, got)
			} else {
				require.Len(t, got, 1)
				require.Contains(t, got[0], tt.want)
			}
		})
	}
}

func TestCheckReferencesFunctionArity(t *testing.T) {
	libs := map[string]*Library{
		"core": {Schema: &LibrarySchema{Functions: map[string]typecheck.FuncSig{
			"format": variadicSig(1),
			"length": fixedSig(1),
		}}},
	}
	cases := []struct {
		name string
		call string
		want string
	}{
		{"variadic one arg", "core.format('%s')", ""},
		{"variadic many args", "core.format('%s-%s', 'a', 'b')", ""},
		{"variadic too few", "core.format()", "core.format takes at least 1 argument, got 0"},
		{"fixed exact", "core.length('hi')", ""},
		{"fixed too few", "core.length()", "core.length takes 1 argument, got 0"},
		{"fixed too many", "core.length('a', 'b')", "core.length takes 1 argument, got 2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "actions: {\n  core: { command: { x: { argv: [" + c.call + "] } } }\n}\n"
			got := checkRefMessages(t, CheckReferences(parseStack(t, src), libs))
			if c.want == "" {
				require.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			require.Contains(t, got[0], c.want)
		})
	}
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

func TestCheckReferencesFunctionArgumentTypes(t *testing.T) {
	strT := typecheck.TString()
	libs := map[string]*Library{
		"core": {Schema: &LibrarySchema{
			Actions: map[string]*TypeSchema{"command": {
				Inputs: map[string]typecheck.Type{
					"argv": typecheck.TList(typecheck.TString()),
				},
			}},
			Functions: map[string]typecheck.FuncSig{
				"b64-encode": {Params: []typecheck.Type{strT}, Result: strT},
				"length": {Params: []typecheck.Type{typecheck.TAny()},
					Result: typecheck.TInteger()},
			},
		}},
	}
	cases := []struct {
		name string
		call string
		want string
	}{
		{"argument type matches", "core.b64-encode('x')", ""},
		{"argument type mismatch", "core.b64-encode(5)", "expected string"},
		{"result type feeds the field", "core.length('x')", "expected string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "actions: {\n  core: { command: { x: { argv: [" + c.call + "] } } }\n}\n"
			errs := CheckReferences(parseStack(t, src), libs)
			var got []string
			for _, e := range errs.Errors() {
				got = append(got, e.Msg)
			}
			if c.want == "" {
				require.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			require.Contains(t, got[0], c.want)
		})
	}
}

func TestCheckReferencesFunctionOnUBLibrary(t *testing.T) {
	libs := map[string]*Library{
		"core": {Schema: &LibrarySchema{Functions: map[string]typecheck.FuncSig{
			"format": variadicSig(1),
		}}},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*CompositeType{
				"pair": {Name: "pair"},
			},
		},
	}
	errs := CheckReferences(parseStack(t, `
actions: {
  core: { command: { x: { argv: [w.fn('hi')] } } }
}
`), libs)
	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0],
		`library "w" is implemented in unobin and exports no functions`)
}

func TestCheckReferencesConstraintRootsLimitedToVar(t *testing.T) {
	strT := typecheck.TString()
	libs := map[string]*Library{
		"core": {Schema: &LibrarySchema{
			Resources: map[string]*TypeSchema{"thing": {
				Outputs: map[string]typecheck.Type{"id": strT},
			}},
			Functions: map[string]typecheck.FuncSig{
				"all": {Params: []typecheck.Type{typecheck.TList(typecheck.TBoolean())},
					Result: typecheck.TBoolean()},
			},
		}},
	}
	src := `
inputs: {
  replicas: { type: optional(list(object({ port: optional(integer) }))) }
}
locals: { limit: 3 }
resources: {
  core: { thing: { x: { name: 'a' } } }
}
constraints: [
  { kind: predicate, when: true, require: resource.core.thing.x.id != null },
  { kind: predicate, when: true, require: local.limit > 0 },
  {
    kind:    predicate
    when:    var.replicas != null
    require: core.all([for r in var.replicas: r.port > 0])
  },
]
`
	errs := CheckReferences(parseStack(t, src), libs)
	var got []string
	for _, e := range errs.Errors() {
		got = append(got, e.Msg)
	}
	require.Len(t, got, 2, "got: %v", got)
	require.Contains(t, got[0],
		"a constraint may read inputs only, not resource.core.thing.x.id")
	require.Contains(t, got[1], "a constraint may read inputs only, not local.limit")
}

func TestCheckReferencesConstraintForEach(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"each allowed under for-each", `
inputs: {
  replicas: { type: optional(list(object({ tls: optional(boolean) }))) }
}
constraints: [
  { kind: predicate, @for-each: var.replicas, when: true, require: @each.value.tls == true },
]
`, ""},
		{"each outside for-each rejected", `
inputs: {
  replicas: { type: optional(list(object({ tls: optional(boolean) }))) }
}
constraints: [
  { kind: predicate, when: true, require: @each.value.tls == true },
]
`, "@each"},
		{"for-each iterable reads inputs only", `
resources: {
  core: { thing: { x: { name: 'a' } } }
}
constraints: [
  { kind: predicate, @for-each: resource.core.thing.x.id, when: true, require: true },
]
`, "a constraint may read inputs only, not resource.core.thing.x.id"},
		{"chained bindings resolve", `
inputs: {
  rules: { type: optional(list(object({ max: optional(number) }))) }
}
constraints: [
  {
    kind: predicate
    @for-each: [
      { @rule: var.rules },
      { @t:    @rule.value.transitions },
    ]
    when:    true
    require: @t.value.days != null && @rule.value.max != null
  },
]
`, ""},
		{"undeclared binding rejected", `
inputs: {
  rules: { type: optional(list(string)) }
}
constraints: [
  {
    kind: predicate
    @for-each: [ { @rule: var.rules } ]
    when:    true
    require: @x.value != null
  },
]
`, "@x is not bound"},
		{"each in a chained entry rejected", `
inputs: {
  rules: { type: optional(list(string)) }
}
constraints: [
  {
    kind: predicate
    @for-each: [ { @rule: var.rules } ]
    when:    true
    require: @each.value != null
  },
]
`, "@each is not bound in a chained @for-each"},
		{"level reads only earlier bindings", `
inputs: {
  rules: { type: optional(list(string)) }
}
constraints: [
  {
    kind: predicate
    @for-each: [
      { @a: @b.value.items },
      { @b: var.rules },
    ]
    when:    true
    require: true
  },
]
`, "@b is not bound"},
		{"level iterable reads inputs only", `
resources: {
  core: { thing: { x: { name: 'a' } } }
}
constraints: [
  {
    kind: predicate
    @for-each: [ { @a: resource.core.thing.x.id } ]
    when:    true
    require: true
  },
]
`, "a constraint may read inputs only, not resource.core.thing.x.id"},
	}
	libs := map[string]*Library{
		"core": {Schema: &LibrarySchema{
			Resources: map[string]*TypeSchema{"thing": {
				Outputs: map[string]typecheck.Type{"id": typecheck.TString()},
			}},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := CheckReferences(parseStack(t, c.src), libs)
			var got []string
			for _, e := range errs.Errors() {
				got = append(got, e.Msg)
			}
			if c.want == "" {
				require.Empty(t, got, "got: %v", got)
				return
			}
			require.Len(t, got, 1, "got: %v", got)
			require.Contains(t, got[0], c.want)
		})
	}
}
