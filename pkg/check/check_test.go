package check

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
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
				"resources: { one: local.file { path: var.p } }\n" +
				"outputs: { bad: { value: resource.one.path[*] } }\n",
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
			errs := checkSyntaxReferences(t, c.stack, nil)
			require.Equal(t, c.want, checkRefMessages(t, errs))
		})
	}
}

func TestCheckReferencesResourceModuleMustBeImported(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: { welcome: greeter.greeting { message: 'hello' } }
`, map[string]*runtime.Library{
		"local": {},
	})

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `library "greeter" is not imported`)
}

func TestCheckReferencesDeclarationCategory(t *testing.T) {
	greeting := parseSyntaxCompositeFixture(t, `
greeting: action {
  inputs:  { message: { type: string } }
  outputs: { said: { value: var.message } }
}
`)
	body := greeting.body
	libs := func() map[string]*runtime.Library {
		return map[string]*runtime.Library{
			"greeter": {ActionComposites: map[string]*runtime.CompositeType{
				"greeting": {Name: "greeting", Kind: runtime.NodeAction, SyntaxBody: &body},
			}},
			"cloud": {Schema: &runtime.LibrarySchema{
				Resources: map[string]*runtime.TypeSchema{"vpc": {}},
				Actions:   map[string]*runtime.TypeSchema{"ping": {}},
			}},
		}
	}
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "action composite used as a resource",
			src:  "resources: { x: greeter.greeting { message: 'hi' } }\n",
			want: []string{`library "greeter" has no resource "greeting"`},
		},
		{
			name: "action composite used as an action",
			src:  "actions: { x: greeter.greeting { message: 'hi' } }\n",
		},
		{
			name: "go action used as a resource",
			src:  "resources: { x: cloud.ping {} }\n",
			want: []string{`library "cloud" has no resource "ping"`},
		},
		{
			name: "go resource used as data",
			src:  "data: { x: cloud.vpc {} }\n",
			want: []string{`library "cloud" has no data "vpc"`},
		},
		{
			name: "go resource used as a resource",
			src:  "resources: { x: cloud.vpc {} }\n",
		},
		{
			name: "unknown type",
			src:  "resources: { x: cloud.nope {} }\n",
			want: []string{`library "cloud" has no resource "nope"`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := checkSyntaxReferences(t, c.src, libs())
			require.Equal(t, c.want, checkRefMessages(t, errs))
		})
	}
}

func TestCheckReferencesCompositeScope(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
file-pair: resource {
  inputs: { path: { type: string } }
  resources: {
    one: local.file { path: var.path, content: 'hello' }
    two: local.file { path: resource.one.path, content: 'world' }
  }
  outputs: { path: { value: resource.two.path } }
}
`)
	body := composite.body
	libs := map[string]*runtime.Library{
		"bundle": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"file-pair": {
					Name:       "file-pair",
					SyntaxBody: &body,
					Libraries:  map[string]*runtime.Library{"local": {}},
				},
			},
		},
	}

	errs := checkSyntaxReferences(t, `
inputs:    { target: { type: string } }
resources: { demo: bundle.file-pair { path: var.target } }
outputs:   { path: { value: resource.demo.path } }
`, libs)

	require.Empty(t, checkRefMessages(t, errs))
}

func TestCheckReferencesCompositeUnknownsUseCompositeScope(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
file-pair: resource {
  inputs:    { path: { type: string } }
  resources: { one: local.file { path: var.missing, content: resource.absent.content } }
  outputs:   { path: { value: resource.one.path } }
}
`)
	body := composite.body
	libs := map[string]*runtime.Library{
		"bundle": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"file-pair": {
					Name:       "file-pair",
					SyntaxBody: &body,
					Libraries:  map[string]*runtime.Library{"local": {}},
				},
			},
		},
	}

	errs := checkSyntaxReferences(t, `
resources: { demo: bundle.file-pair { path: 'x.txt' } }
`, libs)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 2)
	require.Contains(t, got[0], `unknown input "missing"`)
	require.Contains(t, got[1],
		`unknown resource "resource.demo/resource.absent"`)
}

func TestCheckReferencesConstraintPredicateRootScope(t *testing.T) {
	errs := checkSyntaxReferences(t, `
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
`, nil)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 2)
	require.Contains(t, got[0], `unknown input "bogus"`)
	require.Contains(t, got[1], `unknown input "also-missing"`)
}

func TestCheckReferencesConstraintPredicateCompositeScope(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
thing: resource {
  inputs:      { region: { type: string } }
  constraints: [{ kind: predicate, when: var.bogus == 'x', require: var.region == 'y' }]
  resources:   { one: local.file { path: var.region, content: 'hi' } }
  outputs:     { path: { value: resource.one.path } }
}
`)
	body := composite.body
	libs := map[string]*runtime.Library{
		"bundle": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"thing": {
					Name:       "thing",
					SyntaxBody: &body,
					Libraries:  map[string]*runtime.Library{"local": {}},
				},
			},
		},
	}

	errs := checkSyntaxReferences(t, `
resources: { demo: bundle.thing { region: 'us-east-1' } }
`, libs)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown input "bogus"`)
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

func TestCheckReferencesEachScope(t *testing.T) {
	errs := checkSyntaxReferences(t, `
inputs: { files: { type: map(string) } }
resources: {
  many: local.file { @for-each: var.files, path: @each.key, content: @each.value }
  mirror: local.file {
    @for-each: var.files
    path:      resource.many[@each.key].path
    content:   @each.value
  }
  bad: local.file { path: @each.key, content: 'no loop' }
}
`, nil)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `@each is only available inside @for-each`)
}

func TestCheckReferencesUnknownAtRoots(t *testing.T) {
	errs := checkSyntaxReferences(t, `
inputs: { files: { type: map(string) } }
locals: { greeting: @core.greeting }
resources: {
  many: local.file { @for-each: var.files, path: @eech.key, content: @each.value }
  one: local.file { path: @rule.value, content: 'x' }
}
`, nil)

	got := checkRefMessages(t, errs)
	require.Equal(t, []string{
		"@core names functions; call one, e.g. @core.length(...)",
		"@eech is not bound",
		"@rule is not bound",
	}, got)
}

func checkRefMessages(t *testing.T, errs *lang.ErrorList) []string {
	t.Helper()
	for _, err := range errs.Errors() {
		require.Equal(t, lang.ErrResolve, err.Kind)
	}
	return errs.Messages()
}

func TestCheckReferencesConstraintRootsLimitedToVar(t *testing.T) {
	strT := typecheck.TString()
	libs := map[string]*runtime.Library{
		"core": {Schema: &runtime.LibrarySchema{
			Resources: map[string]*runtime.TypeSchema{"thing": {
				Outputs: map[string]typecheck.Type{"id": strT},
			}},
			Functions: map[string]typecheck.FuncSig{
				"all": {Params: []typecheck.Type{typecheck.TList(typecheck.TBoolean())},
					Result: typecheck.TBoolean()},
			},
		}},
	}
	src := `
inputs:    { replicas: { type: optional(list(object({ port: optional(integer) }))) } }
locals:    { limit: 3 }
resources: { x: core.thing { name: 'a' } }
constraints: [
  { kind: predicate, when: true, require: resource.x.id != null },
  { kind: predicate, when: true, require: local.limit > 0 },
  {
    kind:    predicate
    when:    var.replicas != null
    require: core.all([ for r in var.replicas : r.port != null && r.port > 0 ])
  },
]
`
	errs := checkSyntaxReferences(t, src, libs)
	var got []string
	for _, e := range errs.Errors() {
		got = append(got, e.Msg)
	}
	require.Len(t, got, 2, "got: %v", got)
	require.Contains(t, got[0],
		"a constraint may read inputs only, not resource.x.id")
	require.Contains(t, got[1], "a constraint may read inputs only, not local.limit")
}

func TestCheckReferencesUnknownPathRoots(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "dotted unknown root",
			src:  `outputs: { bad: { value: foo.bar } }`,
			want: []string{
				`unknown name "foo"; references start with var, local, ` +
					"resource, data, or action",
			},
		},
		{
			name: "typo of an address root",
			src: `
inputs: { thing: { type: string } }
outputs: { bad: { value: vars.thing } }
`,
			want: []string{
				`unknown name "vars"; references start with var, local, ` +
					"resource, data, or action",
			},
		},
		{
			name: "unknown root in a constraint",
			src: `
constraints: [
  { kind: predicate, when: foo.bar == null, require: true },
]
`,
			want: []string{
				`unknown name "foo"; references start with var, local, ` +
					"resource, data, or action",
			},
		},
		{
			name: "binding root with hyphen suggests subtraction",
			src: `
inputs: { xs: { type: list(integer) } }
outputs: { bad: { value: [ for x in var.xs : x-1.value ] } }
`,
			want: []string{`unknown name "x-1"; write x - 1 to subtract`},
		},
		{
			name: "comprehension binding is a legal root",
			src: `
inputs: { subnets: { type: list(object({ cidr: string })) } }
outputs: { ok: { value: [ for s in var.subnets : s.cidr ] } }
`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			errs := checkSyntaxReferences(t, tt.src, nil)
			var got []string
			for _, e := range errs.Errors() {
				got = append(got, e.Msg)
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCheckReferencesForEachKinds(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "node fan-out over a list",
			src: `
inputs:    { names: { type: list(string) } }
resources: { one: local.file { @for-each: var.names, path: @each.value } }
`,
			want: []string{
				"@for-each: iterable must be a map, got list(string); " +
					"turn a list into a map with { for n in ns : n => n }",
			},
		},
		{
			name: "node fan-out over an optional list",
			src: `
inputs:    { names: { type: optional(list(string)) } }
resources: { one: local.file { @for-each: var.names, path: @each.value } }
`,
			want: []string{
				"@for-each: iterable may be null; supply a fallback, like " +
					"m ?? {} (got optional(list(string)))",
			},
		},
		{
			name: "node fan-out over a narrowed optional map",
			src: `
inputs: { tags: { type: optional(map(string)) } }
resources: {
  one: local.file { @for-each: if var.tags == null then {} else var.tags, path: @each.value }
}
`,
		},
		{
			name: "constraint fan-out over an optional list is vacuous when null",
			src: `
inputs: { names: { type: optional(list(string)) } }
constraints: [
  { kind: predicate, @for-each: var.names, when: true, require: true },
]
`,
		},
		{
			name: "node fan-out over a scalar",
			src: `
inputs:    { name: { type: string } }
resources: { one: local.file { @for-each: var.name, path: @each.value } }
`,
			want: []string{"@for-each: iterable must be a map, got string"},
		},
		{
			name: "node fan-out over a map",
			src: `
inputs:    { tags: { type: map(string) } }
resources: { one: local.file { @for-each: var.tags, path: @each.value } }
`,
		},
		{
			name: "node fan-out over an object",
			src: `
inputs:    { cfg: { type: object({ a: string }) } }
resources: { one: local.file { @for-each: var.cfg, path: @each.key } }
`,
		},
		{
			name: "constraint fan-out over a scalar",
			src: `
inputs: { port: { type: integer } }
constraints: [
  { kind: predicate, @for-each: var.port, when: true, require: true },
]
`,
			want: []string{"@for-each: iterable must be a list or a map, got integer"},
		},
		{
			name: "chained levels skip the kind check",
			src: `
inputs: { port: { type: integer } }
constraints: [
  { kind: predicate, @for-each: [ { @r: var.port } ], when: true, require: true },
]
`,
		},
		{
			name: "node fan-out over bare opaque",
			src: `
inputs:    { blob: { type: opaque } }
resources: { one: local.file { @for-each: var.blob, path: @each.key } }
`,
			want: []string{
				"@for-each: iterable is opaque; declare its type, like map(...)",
			},
		},
		{
			name: "node fan-out over optional opaque",
			src: `
inputs:    { blob: { type: optional(opaque) } }
resources: { one: local.file { @for-each: var.blob, path: @each.key } }
`,
			want: []string{
				"@for-each: iterable is opaque; declare its type, like map(...)",
			},
		},
		{
			name: "node fan-out over a map of opaque",
			src: `
inputs:    { blobs: { type: map(opaque) } }
resources: { one: local.file { @for-each: var.blobs, path: @each.key } }
`,
		},
		{
			name: "constraint fan-out over bare opaque",
			src: `
inputs: { blob: { type: opaque } }
constraints: [
  { kind: predicate, @for-each: var.blob, when: true, require: true },
]
`,
			want: []string{
				"@for-each: iterable is opaque; declare its type, like list(...) or map(...)",
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			errs := checkSyntaxReferences(t, tt.src, nil)
			var got []string
			for _, e := range errs.Errors() {
				got = append(got, e.Msg)
			}
			require.Equal(t, tt.want, got)
		})
	}
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
resources:   { x: core.thing { name: 'a' } }
constraints: [{ kind: predicate, @for-each: resource.x.id, when: true, require: true }]
`, "a constraint may read inputs only, not resource.x.id"},
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
resources: { x: core.thing { name: 'a' } }
constraints: [
  { kind: predicate, @for-each: [{ @a: resource.x.id }], when: true, require: true },
]
`, "a constraint may read inputs only, not resource.x.id"},
	}
	libs := map[string]*runtime.Library{
		"core": {Schema: &runtime.LibrarySchema{
			Resources: map[string]*runtime.TypeSchema{"thing": {
				Outputs: map[string]typecheck.Type{"id": typecheck.TString()},
			}},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := checkSyntaxReferences(t, c.src, libs)
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

func TestCheckReferencesBareIdents(t *testing.T) {
	cases := []struct {
		name  string
		stack string
		want  []string
	}{
		{
			name:  "body field",
			stack: "resources: { one: local.file { path: tcp } }\n",
			want:  []string{`unknown name "tcp"; write 'tcp' for a string`},
		},
		{
			name:  "array element",
			stack: "resources: { one: local.file { args: ['echo', verbose] } }\n",
			want:  []string{`unknown name "verbose"; write 'verbose' for a string`},
		},
		{
			name:  "root output",
			stack: "outputs: { mode: { value: fast } }\n",
			want:  []string{`unknown name "fast"; write 'fast' for a string`},
		},
		{
			name:  "local value",
			stack: "locals: { mode: fast }\n",
			want:  []string{`unknown name "fast"; write 'fast' for a string`},
		},
		{
			name: "comprehension body subtraction",
			stack: "inputs: { nums: { type: list(number) } }\n" +
				"outputs: { dec: { value: [ for n in var.nums : n-1 ] } }\n",
			want: []string{`unknown name "n-1"; write n - 1 to subtract`},
		},
		{
			name: "comprehension binding is bound",
			stack: "inputs: { nums: { type: list(number) } }\n" +
				"outputs: { same: { value: [ for n in var.nums : n ] } }\n",
			want: nil,
		},
		{
			name: "two-name binding is bound",
			stack: "inputs: { tags: { type: map(string) } }\n" +
				"outputs: { copy: { value: { for k, v in var.tags : k => v } } }\n",
			want: nil,
		},
		{
			name: "comprehension source is outside the binding",
			stack: "inputs: { nums: { type: list(number) } }\n" +
				"outputs: { bad: { value: [ for n in n : n ] } }\n",
			want: []string{`unknown name "n"; write 'n' for a string`},
		},
		{
			name: "nested comprehension sees the outer binding",
			stack: "inputs: { xs: { type: list(object({ ys: list(number) })) } }\n" +
				"outputs: { d: { value: [ for a in var.xs : [ for b in a.ys : a-b ] ] } }\n",
			want: []string{`unknown name "a-b"; write a - b to subtract`},
		},
		{
			name: "filter",
			stack: "inputs: { nums: { type: list(number) } }\n" +
				"outputs: { f: { value: [ for n in var.nums : n when active ] } }\n",
			want: []string{`unknown name "active"; write 'active' for a string`},
		},
		{
			name: "conditional branches",
			stack: "inputs: { on: { type: boolean } }\n" +
				"outputs: { pick: { value: if var.on then fast else slow } }\n",
			want: []string{
				`unknown name "fast"; write 'fast' for a string`,
				`unknown name "slow"; write 'slow' for a string`,
			},
		},
		{
			name: "interpolation slot",
			stack: "inputs: { nums: { type: list(number) } }\n" +
				"outputs: { s: { value: [ for n in var.nums : $'{{ n-1 }}' ] } }\n",
			want: []string{`unknown name "n-1"; write n - 1 to subtract`},
		},
		{
			name: "index expression",
			stack: "inputs: { tags: { type: map(string) } }\n" +
				"outputs: { v: { value: var.tags[env] } }\n",
			want: []string{`unknown name "env"; write 'env' for a string`},
		},
		{
			name: "bare each",
			stack: "inputs: { files: { type: map(string) } }\n" +
				"resources: { many: local.file { @for-each: var.files, path: @each } }\n",
			want: []string{`@each cannot stand alone; read @each.key or @each.value`},
		},
		{
			name:  "bare core",
			stack: "outputs: { x: { value: @core } }\n",
			want:  []string{`@core names functions; call one, e.g. @core.length(...)`},
		},
		{
			name: "constraint require",
			stack: "inputs: { tier: { type: string } }\n" +
				"constraints: [ { kind: predicate, when: true, require: ready, " +
				"message: 'm' } ]\n",
			want: []string{`unknown name "ready"; write 'ready' for a string`},
		},
		{
			name: "chain level iterable",
			stack: "constraints: [ { kind: predicate, " +
				"@for-each: [ { @rule: rules } ], " +
				"when: true, require: @rule.value != null, message: 'm' } ]\n",
			want: []string{`unknown name "rules"; write 'rules' for a string`},
		},
		{
			name: "kebab binding keeps the longest prefix",
			stack: "inputs: { nums: { type: list(number) } }\n" +
				"outputs: { d: { value: [ for a-b in var.nums : a-b-1 ] } }\n",
			want: []string{`unknown name "a-b-1"; write a-b - 1 to subtract`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := checkSyntaxReferences(t, c.stack, nil)
			require.Equal(t, c.want, checkRefMessages(t, errs))
		})
	}
}

func TestCheckReferencesHyphenHints(t *testing.T) {
	libs := map[string]*runtime.Library{
		"core": {Schema: &runtime.LibrarySchema{
			Resources: map[string]*runtime.TypeSchema{"thing": {
				Outputs: map[string]typecheck.Type{"size": typecheck.TInteger()},
			}},
		}},
	}
	cases := []struct {
		name  string
		stack string
		want  []string
	}{
		{
			name: "input minus number",
			stack: "inputs: { count: { type: integer } }\n" +
				"outputs: { x: { value: var.count-1 } }\n",
			want: []string{
				`unknown input "count-1"; write var.count - 1 to subtract`,
			},
		},
		{
			name: "input minus input",
			stack: "inputs: { count: { type: integer }, other: { type: integer } }\n" +
				"outputs: { x: { value: var.count-other } }\n",
			want: []string{
				`unknown input "count-other"; write var.count - var.other to subtract`,
			},
		},
		{
			name:  "unknown input without a known prefix",
			stack: "outputs: { x: { value: var.cidr-block } }\n",
			want:  []string{`unknown input "cidr-block"`},
		},
		{
			name: "longest known input prefix wins",
			stack: "inputs: { a-b: { type: integer } }\n" +
				"outputs: { x: { value: var.a-b-1 } }\n",
			want: []string{
				`unknown input "a-b-1"; write var.a-b - 1 to subtract`,
			},
		},
		{
			name: "local minus number",
			stack: "locals: { retries: 3 }\n" +
				"outputs: { x: { value: local.retries-1 } }\n",
			want: []string{
				`unknown local "retries-1"; write local.retries - 1 to subtract`,
			},
		},
		{
			name: "field minus number",
			stack: "resources: { one: core.thing { name: 'a' } }\n" +
				"outputs: { x: { value: resource.one.size-1 } }\n",
			want: []string{
				`unknown field "size-1" on core.thing;` +
					` write resource.one.size - 1 to subtract`,
			},
		},
		{
			name: "unknown field without a known prefix",
			stack: "resources: { one: core.thing { name: 'a' } }\n" +
				"outputs: { x: { value: resource.one.nope } }\n",
			want: []string{`unknown field "nope" on core.thing`},
		},
		{
			name: "each value minus number",
			stack: "inputs: { files: { type: map(string) } }\n" +
				"resources: { many: core.thing { @for-each: var.files, name: @each.value-1 } }\n",
			want: []string{
				`@each.value-1 is not available; write @each.value - 1 to subtract`,
			},
		},
		{
			name: "each segment without a known prefix",
			stack: "inputs: { files: { type: map(string) } }\n" +
				"resources: { many: core.thing { @for-each: var.files, name: @each.foo } }\n",
			want: []string{`@each.foo is not available`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := checkSyntaxReferences(t, c.stack, libs)
			require.Equal(t, c.want, checkRefMessages(t, errs))
		})
	}
}

func TestCheckReferencesNodeCycle(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: {
  a: local.file { path: resource.b.path }
  b: local.file { path: resource.a.path }
}
`, nil)
	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "reference cycle: "+
		"resource.a -> resource.b -> resource.a")
}

func TestCheckReferencesNodeSelfCycle(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: { a: local.file { path: resource.a.path } }
`, nil)
	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0],
		"reference cycle: resource.a -> resource.a")
}
