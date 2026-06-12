package check

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

// constrainedLibs returns a single Go library whose `thing` resource
// requires exactly one of name or size, the same constraint the plan-time
// tests use, so a literal node can be checked against it at compile.
func constrainedLibs() map[string]*runtime.Library {
	return map[string]*runtime.Library{
		"core": {Schema: &runtime.LibrarySchema{
			Resources: map[string]*runtime.TypeSchema{
				"thing": {Constraints: []lang.ConstraintSpec{
					{Kind: "exactly-one-of", Fields: []string{"var.name", "var.size"}},
				}},
				"plain": {},
			},
		}},
	}
}

func TestCheckLiteralConstraints(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "literal violation is reported",
			src:  `resources: { core.thing.x: { name: 'x', size: 1 } }`,
			want: []string{
				"resource.core.thing.x: constraints[0] (exactly-one-of " +
					"[name, size]): expected exactly one to be set, got 2 (name, size)",
			},
		},
		{
			name: "satisfied literal passes",
			src:  `resources: { core.thing.x: { name: 'x' } }`,
			want: nil,
		},
		{
			name: "neither field set is reported",
			src:  `resources: { core.thing.x: { other: 'z' } }`,
			want: []string{
				"resource.core.thing.x: constraints[0] (exactly-one-of " +
					"[name, size]): expected exactly one to be set, got 0 ()",
			},
		},
		{
			name: "input reference in a constrained field defers to plan",
			src: `inputs:    { who: { type: string } }
resources: { core.thing.x: { name: var.who, size: 1 } }`,
			want: nil,
		},
		{
			name: "literal violation is reported despite an unrelated input reference",
			src: `inputs:    { who: { type: string } }
resources: { core.thing.x: { name: 'x', size: 1, region: var.who } }`,
			want: []string{
				"resource.core.thing.x: constraints[0] (exactly-one-of " +
					"[name, size]): expected exactly one to be set, got 2 (name, size)",
			},
		},
		{
			name: "output reference defers to plan",
			src: `resources: {
  core.thing.a: { name: 'a' }
  core.thing.b: { name: resource.core.thing.a.id, size: 1 }
}`,
			want: nil,
		},
		{
			name: "type without constraints passes",
			src:  `resources: { core.plain.x: { name: 'x', size: 1 } }`,
			want: nil,
		},
		{
			name: "unimported alias is skipped",
			src:  `resources: { other.thing.x: { name: 'x', size: 1 } }`,
			want: nil,
		},
		{
			name: "two violations are both reported",
			src:  `resources: { core.thing.x: { name: 'x', size: 1 }, core.thing.y: { name: 'y', size: 2 } }`,
			want: []string{
				"resource.core.thing.x: constraints[0] (exactly-one-of " +
					"[name, size]): expected exactly one to be set, got 2 (name, size)",
				"resource.core.thing.y: constraints[0] (exactly-one-of " +
					"[name, size]): expected exactly one to be set, got 2 (name, size)",
			},
		},
		{
			name: "one literal violation alongside a deferred node",
			src: `inputs:    { who: { type: string } }
resources: { core.thing.x: { name: 'x', size: 1 }, core.thing.y: { name: var.who, size: 2 } }`,
			want: []string{
				"resource.core.thing.x: constraints[0] (exactly-one-of " +
					"[name, size]): expected exactly one to be set, got 2 (name, size)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := checkLiteralConstraints(parseStack(t, tt.src), constrainedLibs())
			require.Equal(t, tt.want, errs.Messages())
		})
	}
}

// TestCheckLiteralConstraintsLengthPredicate proves a lowered length
// condition runs at the compile site: the predicate calls @core.length
// with no import table in the evaluation context, and an explicitly
// empty list literal is caught before plan.
func TestCheckLiteralConstraintsLengthPredicate(t *testing.T) {
	libs := map[string]*runtime.Library{
		"core": {Schema: &runtime.LibrarySchema{
			Resources: map[string]*runtime.TypeSchema{
				"thing": {Constraints: []lang.ConstraintSpec{{
					Kind:    "predicate",
					When:    "true",
					Require: "((var.items != null) && (@core.length(var.items) >= 1))",
					Message: "items must list at least one entry",
				}}},
			},
		}},
	}
	f := parseStack(t, `resources: { core.thing.x: { items: [] } }`)
	errs := checkLiteralConstraints(f, libs)
	require.Equal(t, 1, errs.Len(), "got: %v", errs.Err())
	require.Contains(t, errs.Errors()[0].Error(), "items must list at least one entry")

	ok := checkLiteralConstraints(parseStack(t, `resources: { core.thing.x: { items: ['a'] } }`), libs)
	require.Equal(t, 0, ok.Len(), "got: %v", ok.Err())
}

// TestCheckLiteralConstraintsInsideComposite proves the check reaches
// the nodes inside a composite body: a literal violation there is
// reported under the call site's address, and a conforming body
// passes.
func TestCheckLiteralConstraintsInsideComposite(t *testing.T) {
	compositeLibs := func(node string) map[string]*runtime.Library {
		body := parseStack(t, `
inputs:    { path: { type: string } }
resources: { core.thing.inner: `+node+` }
outputs:   { id: { value: resource.core.thing.inner.id } }
`)
		return map[string]*runtime.Library{
			"bundle": {ResourceComposites: map[string]*runtime.CompositeType{
				"file-pair": {
					Name:      "file-pair",
					Body:      body,
					Libraries: constrainedLibs(),
				},
			}},
		}
	}
	root := `resources: { bundle.file-pair.demo: { path: 'x.txt' } }`

	errs := checkLiteralConstraints(parseStack(t, root),
		compositeLibs(`{ name: 'x', size: 1 }`))
	require.Equal(t, []string{
		"resource.bundle.file-pair.demo/resource.core.thing.inner: " +
			"constraints[0] (exactly-one-of [name, size]): " +
			"expected exactly one to be set, got 2 (name, size)",
	}, errs.Messages())

	ok := checkLiteralConstraints(parseStack(t, root), compositeLibs(`{ name: 'x' }`))
	require.Empty(t, ok.Messages())
}

// TestCheckLiteralConstraintsDeterministic runs each case repeatedly and
// requires byte-identical messages, so map iteration order cannot leak
// into the reported diagnostics.
func TestCheckLiteralConstraintsDeterministic(t *testing.T) {
	src := `resources: {
  core.thing.x: { name: 'x', size: 1 }
  core.thing.y: { name: 'y', size: 2 }
  core.thing.z: { other: 'z' }
}`
	libs := constrainedLibs()
	first := checkLiteralConstraints(parseStack(t, src), libs).Messages()
	require.Len(t, first, 3)
	for range 20 {
		require.Equal(t, first, checkLiteralConstraints(parseStack(t, src), libs).Messages())
	}
}

func TestLiteralValues(t *testing.T) {
	tests := []struct {
		name         string
		src          string
		want         map[string]any
		wantDeferred map[string]bool
	}{
		{
			name: "all scalar literals",
			src:  `{ name: 'x', size: 1, on: true }`,
			want: map[string]any{"name": "x", "size": int64(1), "on": true},
		},
		{
			name: "arithmetic reduces",
			src:  `{ size: 1 + 2 }`,
			want: map[string]any{"size": int64(3)},
		},
		{
			name: "empty body",
			src:  `{}`,
			want: map[string]any{},
		},
		{
			name: "meta field is skipped",
			src:  `{ name: 'x', @lock: 'shared' }`,
			want: map[string]any{"name": "x"},
		},
		{
			name:         "input reference defers its field",
			src:          `{ name: var.who }`,
			want:         map[string]any{},
			wantDeferred: map[string]bool{"name": true},
		},
		{
			name:         "output reference defers its field",
			src:          `{ name: resource.core.thing.a.id }`,
			want:         map[string]any{},
			wantDeferred: map[string]bool{"name": true},
		},
		{
			name:         "nested reference defers its field",
			src:          `{ tags: { owner: var.who } }`,
			want:         map[string]any{},
			wantDeferred: map[string]bool{"tags": true},
		},
		{
			name:         "literal fields reduce alongside a deferred one",
			src:          `{ name: 'x', size: var.n }`,
			want:         map[string]any{"name": "x"},
			wantDeferred: map[string]bool{"size": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, deferred, ok := literalValues(parseValue(t, tt.src))
			require.True(t, ok)
			require.Equal(t, tt.want, values)
			if tt.wantDeferred == nil {
				tt.wantDeferred = map[string]bool{}
			}
			require.Equal(t, tt.wantDeferred, deferred)
		})
	}
}

func TestLiteralValuesNonObjectBody(t *testing.T) {
	_, _, ok := literalValues(parseValue(t, `'not an object'`))
	require.False(t, ok)
}

// checkLiteralMsgs runs the compile-time check against one core.thing
// resource whose type carries specs and whose body is the given literal,
// returning the address-and-constraint messages.
func checkLiteralMsgs(t *testing.T, specs []lang.ConstraintSpec, body string) []string {
	t.Helper()
	libs := map[string]*runtime.Library{
		"core": {Schema: &runtime.LibrarySchema{
			Resources: map[string]*runtime.TypeSchema{"thing": {Constraints: specs}},
		}},
	}
	src := "resources: {\n  core.thing.x: " + body + "\n}\n"
	return checkLiteralConstraints(parseStack(t, src), libs).Messages()
}

// TestCheckLiteralConstraintKinds covers the constraint kinds and the
// predicate path through the compile-time check, including a predicate
// that reads an input the body omits: the check must fill it with null so
// the condition evaluates instead of failing.
func TestCheckLiteralConstraintKinds(t *testing.T) {
	const addr = "resource.core.thing.x: "
	pred := []lang.ConstraintSpec{{
		Kind: "predicate", When: "var.name != null", Require: "var.size != null",
	}}
	tests := []struct {
		name  string
		specs []lang.ConstraintSpec
		body  string
		want  []string
	}{
		{
			name: "at-least-one-of with none set is reported",
			specs: []lang.ConstraintSpec{{Kind: "at-least-one-of",
				Fields: []string{"var.name", "var.size"}}},
			body: `{ region: 'us' }`,
			want: []string{addr + "constraints[0] (at-least-one-of [name, size]): " +
				"expected at least one to be set, got none"},
		},
		{
			name: "required-together with one set is reported",
			specs: []lang.ConstraintSpec{{Kind: "required-together",
				Fields: []string{"var.name", "var.size"}}},
			body: `{ name: 'a' }`,
			want: []string{addr + "constraints[0] (required-together [name, size]): " +
				"expected all set or all null, got 1 set (name)"},
		},
		{
			name:  "predicate with unmet requirement is reported",
			specs: pred,
			body:  `{ name: 'a' }`,
			want:  []string{addr + "constraints[0] (predicate): predicate requirement not satisfied"},
		},
		{
			name:  "predicate with met requirement passes",
			specs: pred,
			body:  `{ name: 'a', size: 1 }`,
			want:  nil,
		},
		{
			name:  "predicate whose condition is false passes",
			specs: pred,
			body:  `{ size: 1 }`,
			want:  nil,
		},
		{
			name: "splat constraint names the violating element",
			specs: []lang.ConstraintSpec{{Kind: "exactly-one-of",
				Fields: []string{"var.items[*].a", "var.items[*].b"}}},
			body: `{ items: [{ a: 1 }, { a: 1, b: 2 }] }`,
			want: []string{addr + "constraints[0] (exactly-one-of [items[1].a, items[1].b]): " +
				"expected exactly one to be set, got 2 (items[1].a, items[1].b)"},
		},
		{
			name: "splat constraint passes when every element conforms",
			specs: []lang.ConstraintSpec{{Kind: "exactly-one-of",
				Fields: []string{"var.items[*].a", "var.items[*].b"}}},
			body: `{ items: [{ a: 1 }, { b: 2 }] }`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, checkLiteralMsgs(t, tt.specs, tt.body))
		})
	}
}

// TestCheckLiteralConstraintsPartialBody covers bodies that mix literal
// and deferred fields: a constraint checks at compile when every field
// it references is literal, defers when any is not, and keeps its
// position in the type's constraint list either way.
func TestCheckLiteralConstraintsPartialBody(t *testing.T) {
	const addr = "resource.core.thing.x: "
	tests := []struct {
		name  string
		specs []lang.ConstraintSpec
		body  string
		want  []string
	}{
		{
			name: "set constraint referencing a deferred field defers",
			specs: []lang.ConstraintSpec{{Kind: "exactly-one-of",
				Fields: []string{"var.name", "var.size"}}},
			body: `{ name: var.who, size: 1 }`,
			want: nil,
		},
		{
			name: "deferred entry keeps the next entry's index",
			specs: []lang.ConstraintSpec{
				{Kind: "required-together", Fields: []string{"var.region", "var.zone"}},
				{Kind: "exactly-one-of", Fields: []string{"var.name", "var.size"}},
			},
			body: `{ name: 'a', size: 1, region: var.who }`,
			want: []string{addr + "constraints[1] (exactly-one-of [name, size]): " +
				"expected exactly one to be set, got 2 (name, size)"},
		},
		{
			name: "predicate over literal fields checks despite a deferred field",
			specs: []lang.ConstraintSpec{{
				Kind: "predicate", When: "var.name != null", Require: "var.size != null",
			}},
			body: `{ name: 'a', region: var.who }`,
			want: []string{addr + "constraints[0] (predicate): predicate requirement not satisfied"},
		},
		{
			name: "predicate reading a deferred field in when defers",
			specs: []lang.ConstraintSpec{{
				Kind: "predicate", When: "var.name != null", Require: "var.size != null",
			}},
			body: `{ name: var.who, size: 1 }`,
			want: nil,
		},
		{
			name: "predicate reading a deferred field in require defers",
			specs: []lang.ConstraintSpec{{
				Kind: "predicate", When: "true", Require: "var.size != null",
			}},
			body: `{ size: var.n }`,
			want: nil,
		},
		{
			name: "iterating predicate over a literal list checks despite a deferred field",
			specs: []lang.ConstraintSpec{{
				Kind: "predicate", ForEach: "var.items",
				When: "true", Require: "@each.value.a != null",
				Message: "a is required",
			}},
			body: `{ items: [{ a: 1 }, { b: 2 }], region: var.who }`,
			want: []string{addr + "constraints[0] (predicate): a is required (items[1])"},
		},
		{
			name: "iterating predicate over a deferred list defers",
			specs: []lang.ConstraintSpec{{
				Kind: "predicate", ForEach: "var.items",
				When: "true", Require: "@each.value.a != null",
			}},
			body: `{ items: var.who }`,
			want: nil,
		},
		{
			name: "splat constraint over a literal list checks despite a deferred field",
			specs: []lang.ConstraintSpec{{Kind: "exactly-one-of",
				Fields: []string{"var.items[*].a", "var.items[*].b"}}},
			body: `{ items: [{ a: 1, b: 2 }], region: var.who }`,
			want: []string{addr + "constraints[0] (exactly-one-of [items[0].a, items[0].b]): " +
				"expected exactly one to be set, got 2 (items[0].a, items[0].b)"},
		},
		{
			name: "splat constraint over a deferred list defers",
			specs: []lang.ConstraintSpec{{Kind: "exactly-one-of",
				Fields: []string{"var.items[*].a", "var.items[*].b"}}},
			body: `{ items: var.who }`,
			want: nil,
		},
		{
			name: "constraint over absent fields checks despite a deferred field",
			specs: []lang.ConstraintSpec{{Kind: "at-least-one-of",
				Fields: []string{"var.name", "var.size"}}},
			body: `{ region: var.who }`,
			want: []string{addr + "constraints[0] (at-least-one-of [name, size]): " +
				"expected at least one to be set, got none"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, checkLiteralMsgs(t, tt.specs, tt.body))
		})
	}
}
