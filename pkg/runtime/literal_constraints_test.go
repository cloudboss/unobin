package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

// constrainedLibs returns a single Go library whose `thing` resource
// requires exactly one of name or size, the same constraint the plan-time
// tests use, so a literal node can be checked against it at compile.
func constrainedLibs() map[string]*Library {
	return map[string]*Library{
		"core": {Schema: &LibrarySchema{
			Resources: map[string]*TypeSchema{
				"thing": {Constraints: []lang.ConstraintSpec{
					{Kind: "exactly-one-of", Fields: []string{"name", "size"}},
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
			src: `resources: {
  core: { thing: { x: { name: 'x', size: 1 } } }
}`,
			want: []string{
				"resource.core.thing.x: constraints[0] (exactly-one-of " +
					"[name, size]): expected exactly one to be set, got 2 (name, size)",
			},
		},
		{
			name: "satisfied literal passes",
			src: `resources: {
  core: { thing: { x: { name: 'x' } } }
}`,
			want: nil,
		},
		{
			name: "neither field set is reported",
			src: `resources: {
  core: { thing: { x: { other: 'z' } } }
}`,
			want: []string{
				"resource.core.thing.x: constraints[0] (exactly-one-of " +
					"[name, size]): expected exactly one to be set, got 0 ()",
			},
		},
		{
			name: "input reference defers to plan",
			src: `inputs: {
  who: { type: string }
}
resources: {
  core: { thing: { x: { name: var.who, size: 1 } } }
}`,
			want: nil,
		},
		{
			name: "output reference defers to plan",
			src: `resources: {
  core: {
    thing: {
      a: { name: 'a' }
      b: { name: resource.core.thing.a.id, size: 1 }
    }
  }
}`,
			want: nil,
		},
		{
			name: "type without constraints passes",
			src: `resources: {
  core: { plain: { x: { name: 'x', size: 1 } } }
}`,
			want: nil,
		},
		{
			name: "unimported alias is skipped",
			src: `resources: {
  other: { thing: { x: { name: 'x', size: 1 } } }
}`,
			want: nil,
		},
		{
			name: "two violations are both reported",
			src: `resources: {
  core: {
    thing: {
      x: { name: 'x', size: 1 }
      y: { name: 'y', size: 2 }
    }
  }
}`,
			want: []string{
				"resource.core.thing.x: constraints[0] (exactly-one-of " +
					"[name, size]): expected exactly one to be set, got 2 (name, size)",
				"resource.core.thing.y: constraints[0] (exactly-one-of " +
					"[name, size]): expected exactly one to be set, got 2 (name, size)",
			},
		},
		{
			name: "one literal violation alongside a deferred node",
			src: `inputs: {
  who: { type: string }
}
resources: {
  core: {
    thing: {
      x: { name: 'x', size: 1 }
      y: { name: var.who, size: 2 }
    }
  }
}`,
			want: []string{
				"resource.core.thing.x: constraints[0] (exactly-one-of " +
					"[name, size]): expected exactly one to be set, got 2 (name, size)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := CheckLiteralConstraints(parseStack(t, tt.src), constrainedLibs())
			require.Equal(t, tt.want, constraintMessages(errs))
		})
	}
}

// TestCheckLiteralConstraintsDeterministic runs each case repeatedly and
// requires byte-identical messages, so map iteration order cannot leak
// into the reported diagnostics.
func TestCheckLiteralConstraintsDeterministic(t *testing.T) {
	src := `resources: {
  core: {
    thing: {
      x: { name: 'x', size: 1 }
      y: { name: 'y', size: 2 }
      z: { other: 'z' }
    }
  }
}`
	libs := constrainedLibs()
	first := constraintMessages(CheckLiteralConstraints(parseStack(t, src), libs))
	require.Len(t, first, 3)
	for range 20 {
		require.Equal(t, first, constraintMessages(CheckLiteralConstraints(parseStack(t, src), libs)))
	}
}

func TestLiteralValues(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want map[string]any
		ok   bool
	}{
		{
			name: "all scalar literals",
			src:  `{ name: 'x', size: 1, on: true }`,
			want: map[string]any{"name": "x", "size": int64(1), "on": true},
			ok:   true,
		},
		{
			name: "arithmetic reduces",
			src:  `{ size: 1 + 2 }`,
			want: map[string]any{"size": int64(3)},
			ok:   true,
		},
		{
			name: "empty body",
			src:  `{}`,
			want: map[string]any{},
			ok:   true,
		},
		{
			name: "meta field is skipped",
			src:  `{ name: 'x', @lock: 'shared' }`,
			want: map[string]any{"name": "x"},
			ok:   true,
		},
		{
			name: "input reference is not literal",
			src:  `{ name: var.who }`,
			ok:   false,
		},
		{
			name: "output reference is not literal",
			src:  `{ name: resource.core.thing.a.id }`,
			ok:   false,
		},
		{
			name: "nested reference is not literal",
			src:  `{ tags: { owner: var.who } }`,
			ok:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := literalValues(parseValue(t, tt.src))
			require.Equal(t, tt.ok, ok)
			if tt.ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

// constraintMessages strips the file:line:col and `schema:` prefixes from
// each diagnostic, leaving the address-and-constraint detail the tests
// pin, so they need not track exact source positions.
func constraintMessages(errs *lang.ErrorList) []string {
	var out []string
	for _, e := range errs.Errors() {
		out = append(out, e.Msg)
	}
	return out
}

// checkLiteralMsgs runs the compile-time check against one core.thing
// resource whose type carries specs and whose body is the given literal,
// returning the address-and-constraint messages.
func checkLiteralMsgs(t *testing.T, specs []lang.ConstraintSpec, body string) []string {
	t.Helper()
	libs := map[string]*Library{
		"core": {Schema: &LibrarySchema{
			Resources: map[string]*TypeSchema{"thing": {Constraints: specs}},
		}},
	}
	src := "resources: {\n  core: { thing: { x: " + body + " } }\n}\n"
	return constraintMessages(CheckLiteralConstraints(parseStack(t, src), libs))
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
			name:  "at-least-one-of with none set is reported",
			specs: []lang.ConstraintSpec{{Kind: "at-least-one-of", Fields: []string{"name", "size"}}},
			body:  `{ region: 'us' }`,
			want: []string{addr + "constraints[0] (at-least-one-of [name, size]): " +
				"expected at least one to be set, got none"},
		},
		{
			name:  "required-together with one set is reported",
			specs: []lang.ConstraintSpec{{Kind: "required-together", Fields: []string{"name", "size"}}},
			body:  `{ name: 'a' }`,
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
				Fields: []string{"items[*].a", "items[*].b"}}},
			body: `{ items: [{ a: 1 }, { a: 1, b: 2 }] }`,
			want: []string{addr + "constraints[0] (exactly-one-of [items[1].a, items[1].b]): " +
				"expected exactly one to be set, got 2 (items[1].a, items[1].b)"},
		},
		{
			name: "splat constraint passes when every element conforms",
			specs: []lang.ConstraintSpec{{Kind: "exactly-one-of",
				Fields: []string{"items[*].a", "items[*].b"}}},
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
