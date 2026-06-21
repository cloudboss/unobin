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
