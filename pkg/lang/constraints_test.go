package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// boolExprEval evaluates a tiny set of expressions used by the
// predicate tests: bool literals, equality and inequality between
// var.X and a literal. Real wiring uses runtime.Eval against an
// EvalContext seeded with the validated inputs.
func boolExprEval(values map[string]any) EvalFunc {
	return func(e Expr) (any, error) {
		switch v := e.(type) {
		case *BoolLit:
			return v.Value, nil
		case *Infix:
			left, err := evalLeaf(v.Left, values)
			if err != nil {
				return nil, err
			}
			right, err := evalLeaf(v.Right, values)
			if err != nil {
				return nil, err
			}
			switch v.Op {
			case "==":
				return left == right, nil
			case "!=":
				return left != right, nil
			}
		}
		return nil, nil
	}
}

func evalLeaf(e Expr, values map[string]any) (any, error) {
	switch v := e.(type) {
	case *StringLit:
		return v.Value, nil
	case *NumberLit:
		if v.IsFloat {
			return v.ParsedFloat, nil
		}
		return v.ParsedInt, nil
	case *BoolLit:
		return v.Value, nil
	case *DotPath:
		if v.Root.Name != "var" || len(v.Segments) != 1 {
			return nil, nil
		}
		return values[v.Segments[0].Name], nil
	}
	return nil, nil
}

func TestCheckExactlyOneOf(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: exactly-one-of, fields: [var.a, var.b, var.c] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"a": "x", "b": nil, "c": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"a": nil, "b": nil, "c": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "exactly-one-of")
	require.Contains(t, errs.Err().Error(), "got 0")

	errs = CheckConstraints(block, map[string]any{
		"a": "x", "b": "y", "c": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "got 2")
}

func TestCheckAtLeastOneOf(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: at-least-one-of, fields: [var.a, var.b] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"a": nil, "b": "x",
	}, nil, DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"a": nil, "b": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "at-least-one-of")
}

func TestCheckAtMostOneOf(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: at-most-one-of, fields: [var.a, var.b, var.c] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"a": nil, "b": nil, "c": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"a": "x", "b": nil, "c": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"a": "x", "b": "y", "c": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "at most one")
}

func TestCheckMutuallyExclusive(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: mutually-exclusive, fields: [var.a, var.b] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"a": "x", "b": "y",
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "mutually-exclusive")
}

func TestCheckRequiredTogether(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: required-together, fields: [var.vpc-id, var.subnet-ids] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"vpc-id": "vpc-abc", "subnet-ids": []any{"a"},
	}, nil, DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"vpc-id": nil, "subnet-ids": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"vpc-id": "vpc-abc", "subnet-ids": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "required-together")
}

func TestCheckRequiredWith(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: required-with, fields: [var.trigger, var.dep1, var.dep2] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"trigger": nil, "dep1": nil, "dep2": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"trigger": "x", "dep1": "a", "dep2": "b",
	}, nil, DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"trigger": "x", "dep1": "a", "dep2": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "required-with")
	require.Contains(t, errs.Err().Error(), "missing var.dep2")
}

func TestCheckForbiddenWith(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: forbidden-with, fields: [var.use-spot, var.reserved-capacity] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"use-spot": true, "reserved-capacity": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"use-spot": true, "reserved-capacity": int64(10),
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "forbidden-with")
}

func TestCheckConstraintsNestedFields(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: exactly-one-of, fields: [var.code.inline, var.code.from-file] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"code": map[string]any{"inline": "x"},
	}, nil, DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"code": map[string]any{"inline": "x", "from-file": "y"},
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "got 2")

	errs = CheckConstraints(block, map[string]any{
		"code": map[string]any{},
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "got 0")
}

func TestCheckConstraintsSplatFields(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: exactly-one-of, fields: [var.replicas[*].inline, var.replicas[*].from-file] },
]
`)
	errs := CheckConstraints(block, map[string]any{
		"replicas": []any{
			map[string]any{"inline": "a"},
			map[string]any{"inline": "a", "from-file": "f"},
		},
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len(), errs.Err())
	require.Contains(t, errs.Err().Error(), "var.replicas[1].inline")
}

func TestCheckConstraintsIndexedFields(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: required-together, fields: [var.listeners[0].cert, var.listeners[0].key] },
]
`)
	errs := CheckConstraints(block, map[string]any{
		"listeners": []any{
			map[string]any{"cert": "c"},
		},
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len(), errs.Err())
	require.Contains(t, errs.Err().Error(), "got 1 set (var.listeners[0].cert)")
}

func TestCheckPredicateWhenFalseSkipsRequire(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  {
    kind:    predicate
    when:    var.region == 'us-gov-east-1'
    require: var.fips-mode == true
    message: 'GovCloud regions require FIPS mode enabled'
  },
]
`)
	values := map[string]any{
		"region":    "us-east-1",
		"fips-mode": false,
	}
	errs := CheckConstraints(block, values, boolExprEval(values), DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())
}

func TestCheckPredicateWhenTrueRequireFails(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  {
    kind:    predicate
    when:    var.region == 'us-gov-east-1'
    require: var.fips-mode == true
    message: 'GovCloud regions require FIPS mode enabled'
  },
]
`)
	values := map[string]any{
		"region":    "us-gov-east-1",
		"fips-mode": false,
	}
	errs := CheckConstraints(block, values, boolExprEval(values), DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(),
		"GovCloud regions require FIPS mode enabled")
}

func TestCheckPredicateWhenTrueRequireOK(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  {
    kind:    predicate
    when:    var.region == 'us-gov-east-1'
    require: var.fips-mode == true
  },
]
`)
	values := map[string]any{
		"region":    "us-gov-east-1",
		"fips-mode": true,
	}
	errs := CheckConstraints(block, values, boolExprEval(values), DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())
}

func TestCheckPredicateNoMessageDefaults(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  {
    kind:    predicate
    when:    true
    require: false
  },
]
`)
	errs := CheckConstraints(block, map[string]any{}, boolExprEval(nil), DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "predicate requirement not satisfied")
}

func TestCheckConstraintEntries(t *testing.T) {
	ab := []string{"var.a", "var.b"}
	tests := []struct {
		name    string
		entry   ConstraintEntry
		values  map[string]any
		wantErr bool
	}{
		{"exactly-one one set", ConstraintEntry{Kind: "exactly-one-of", Fields: ab},
			map[string]any{"a": 1}, false},
		{"exactly-one none set", ConstraintEntry{Kind: "exactly-one-of", Fields: ab},
			map[string]any{}, true},
		{"exactly-one two set", ConstraintEntry{Kind: "exactly-one-of", Fields: ab},
			map[string]any{"a": 1, "b": 2}, true},
		{"at-least one set", ConstraintEntry{Kind: "at-least-one-of", Fields: ab},
			map[string]any{"b": 1}, false},
		{"at-least none set", ConstraintEntry{Kind: "at-least-one-of", Fields: ab},
			map[string]any{}, true},
		{"at-most none set", ConstraintEntry{Kind: "at-most-one-of", Fields: ab},
			map[string]any{}, false},
		{"at-most one set", ConstraintEntry{Kind: "at-most-one-of", Fields: ab},
			map[string]any{"a": 1}, false},
		{"at-most two set", ConstraintEntry{Kind: "at-most-one-of", Fields: ab},
			map[string]any{"a": 1, "b": 2}, true},
		{"together all set", ConstraintEntry{Kind: "required-together", Fields: ab},
			map[string]any{"a": 1, "b": 2}, false},
		{"together none set", ConstraintEntry{Kind: "required-together", Fields: ab},
			map[string]any{}, false},
		{"together partial", ConstraintEntry{Kind: "required-together", Fields: ab},
			map[string]any{"a": 1}, true},
		{"with trigger unset", ConstraintEntry{Kind: "required-with", Fields: ab},
			map[string]any{"b": 1}, false},
		{"with trigger and dep", ConstraintEntry{Kind: "required-with", Fields: ab},
			map[string]any{"a": 1, "b": 2}, false},
		{"with trigger no dep", ConstraintEntry{Kind: "required-with", Fields: ab},
			map[string]any{"a": 1}, true},
		{"forbidden trigger unset", ConstraintEntry{Kind: "forbidden-with", Fields: ab},
			map[string]any{"b": 1}, false},
		{"forbidden trigger only", ConstraintEntry{Kind: "forbidden-with", Fields: ab},
			map[string]any{"a": 1}, false},
		{"forbidden both set", ConstraintEntry{Kind: "forbidden-with", Fields: ab},
			map[string]any{"a": 1, "b": 2}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := CheckConstraintEntries([]ConstraintEntry{tt.entry}, tt.values, nil, DisplayRooted)
			if tt.wantErr {
				require.Positive(t, errs.Len())
			} else {
				require.Equal(t, 0, errs.Len(), "unexpected: %v", errs.Err())
			}
		})
	}
}

func TestCheckConstraintEntriesNestedFields(t *testing.T) {
	io := []string{"var.code.inline", "var.code.from-file"}
	tests := []struct {
		name    string
		entry   ConstraintEntry
		values  map[string]any
		wantErr bool
	}{
		{"exactly-one nested one set",
			ConstraintEntry{Kind: "exactly-one-of", Fields: io},
			map[string]any{"code": map[string]any{"inline": "x"}}, false},
		{"exactly-one nested none set",
			ConstraintEntry{Kind: "exactly-one-of", Fields: io},
			map[string]any{"code": map[string]any{}}, true},
		{"exactly-one nested two set",
			ConstraintEntry{Kind: "exactly-one-of", Fields: io},
			map[string]any{"code": map[string]any{"inline": "x", "from-file": "y"}}, true},
		{"exactly-one nested parent absent",
			ConstraintEntry{Kind: "exactly-one-of", Fields: io},
			map[string]any{}, true},
		{"exactly-one nested parent null",
			ConstraintEntry{Kind: "exactly-one-of", Fields: io},
			map[string]any{"code": nil}, true},
		{"at-least nested one set",
			ConstraintEntry{Kind: "at-least-one-of", Fields: io},
			map[string]any{"code": map[string]any{"from-file": "y"}}, false},
		{"at-most nested two set",
			ConstraintEntry{Kind: "at-most-one-of", Fields: io},
			map[string]any{"code": map[string]any{"inline": "x", "from-file": "y"}}, true},
		{"required-with nested trigger and dep",
			ConstraintEntry{Kind: "required-with", Fields: io},
			map[string]any{"code": map[string]any{"inline": "x", "from-file": "y"}}, false},
		{"required-with nested trigger no dep",
			ConstraintEntry{Kind: "required-with", Fields: io},
			map[string]any{"code": map[string]any{"inline": "x"}}, true},
		{"forbidden-with nested both set",
			ConstraintEntry{Kind: "forbidden-with", Fields: io},
			map[string]any{"code": map[string]any{"inline": "x", "from-file": "y"}}, true},
		{"forbidden-with nested trigger only",
			ConstraintEntry{Kind: "forbidden-with", Fields: io},
			map[string]any{"code": map[string]any{"inline": "x"}}, false},
		{"mixed flat and nested both set",
			ConstraintEntry{Kind: "required-together", Fields: []string{"var.name", "var.code.inline"}},
			map[string]any{"name": "db", "code": map[string]any{"inline": "x"}}, false},
		{"mixed flat set nested unset",
			ConstraintEntry{Kind: "required-together", Fields: []string{"var.name", "var.code.inline"}},
			map[string]any{"name": "db", "code": map[string]any{}}, true},
		{"three-level nested set",
			ConstraintEntry{Kind: "at-least-one-of", Fields: []string{"var.code.signing.key-arn"}},
			map[string]any{"code": map[string]any{
				"signing": map[string]any{"key-arn": "arn"}}}, false},
		{"three-level nested unset",
			ConstraintEntry{Kind: "at-least-one-of", Fields: []string{"var.code.signing.key-arn"}},
			map[string]any{"code": map[string]any{"signing": map[string]any{}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := CheckConstraintEntries([]ConstraintEntry{tt.entry}, tt.values, nil, DisplayRooted)
			if tt.wantErr {
				require.Positive(t, errs.Len())
			} else {
				require.Equal(t, 0, errs.Len(), "unexpected: %v", errs.Err())
			}
		})
	}
}

func TestCheckConstraintEntriesIndexedFields(t *testing.T) {
	values := map[string]any{
		"name": "lb",
		"listeners": []any{
			map[string]any{"cert": "c", "key": "k"},
			map[string]any{"cert": "c"},
		},
	}
	tests := []struct {
		name    string
		entry   ConstraintEntry
		wantErr bool
	}{
		{"together both set",
			ConstraintEntry{Kind: "required-together",
				Fields: []string{"var.listeners[0].cert", "var.listeners[0].key"}}, false},
		{"together partial",
			ConstraintEntry{Kind: "required-together",
				Fields: []string{"var.listeners[1].cert", "var.listeners[1].key"}}, true},
		{"exactly-one one set",
			ConstraintEntry{Kind: "exactly-one-of",
				Fields: []string{"var.listeners[1].cert", "var.listeners[1].key"}}, false},
		{"exactly-one out of range reads null",
			ConstraintEntry{Kind: "exactly-one-of",
				Fields: []string{"var.listeners[5].cert", "var.listeners[5].key"}}, true},
		{"together out of range all null",
			ConstraintEntry{Kind: "required-together",
				Fields: []string{"var.listeners[5].cert", "var.listeners[5].key"}}, false},
		{"at-most two set",
			ConstraintEntry{Kind: "at-most-one-of",
				Fields: []string{"var.listeners[0].cert", "var.listeners[0].key"}}, true},
		{"mixed indexed trigger with flat dep",
			ConstraintEntry{Kind: "required-with",
				Fields: []string{"var.listeners[0].cert", "var.name"}}, false},
		{"mixed indexed trigger forbids flat",
			ConstraintEntry{Kind: "forbidden-with",
				Fields: []string{"var.listeners[0].cert", "var.name"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var outputs []string
			for range 3 {
				errs := CheckConstraintEntries([]ConstraintEntry{tt.entry}, values, nil, DisplayRooted)
				outputs = append(outputs, errorText(errs))
			}
			require.Equal(t, outputs[0], outputs[1], "output must be deterministic")
			require.Equal(t, outputs[0], outputs[2], "output must be deterministic")
			if tt.wantErr {
				require.NotEmpty(t, outputs[0])
			} else {
				require.Empty(t, outputs[0])
			}
		})
	}
}

func errorText(errs *ErrorList) string {
	if errs.Len() == 0 {
		return ""
	}
	return errs.Err().Error()
}

func TestCheckConstraintEntriesSplatFields(t *testing.T) {
	io := []string{"var.replicas[*].inline", "var.replicas[*].from-file"}
	ab := []string{"var.replicas[*].a", "var.replicas[*].b"}
	certKey := []string{"var.replicas[*].cert", "var.replicas[*].key"}
	tests := []struct {
		name     string
		entry    ConstraintEntry
		values   map[string]any
		wantMsgs []string
	}{
		{"exactly-one per element all pass",
			ConstraintEntry{Kind: "exactly-one-of", Fields: io},
			map[string]any{"replicas": []any{
				map[string]any{"inline": "a"},
				map[string]any{"from-file": "f"},
			}},
			nil},
		{"exactly-one element both set",
			ConstraintEntry{Kind: "exactly-one-of", Fields: io},
			map[string]any{"replicas": []any{
				map[string]any{"inline": "a"},
				map[string]any{"inline": "a", "from-file": "f"},
			}},
			[]string{
				"constraints[0] (exactly-one-of [var.replicas[1].inline, var.replicas[1].from-file]): " +
					"expected exactly one to be set, got 2 " +
					"(var.replicas[1].inline, var.replicas[1].from-file)",
			}},
		{"exactly-one element neither set",
			ConstraintEntry{Kind: "exactly-one-of", Fields: io},
			map[string]any{"replicas": []any{
				map[string]any{},
				map[string]any{"inline": "a"},
			}},
			[]string{
				"constraints[0] (exactly-one-of [var.replicas[0].inline, var.replicas[0].from-file]): " +
					"expected exactly one to be set, got 0 ()",
			}},
		{"exactly-one multiple elements fail",
			ConstraintEntry{Kind: "exactly-one-of", Fields: io},
			map[string]any{"replicas": []any{
				map[string]any{},
				map[string]any{"inline": "a", "from-file": "f"},
				map[string]any{"inline": "x"},
			}},
			[]string{
				"constraints[0] (exactly-one-of [var.replicas[0].inline, var.replicas[0].from-file]): " +
					"expected exactly one to be set, got 0 ()",
				"constraints[0] (exactly-one-of [var.replicas[1].inline, var.replicas[1].from-file]): " +
					"expected exactly one to be set, got 2 " +
					"(var.replicas[1].inline, var.replicas[1].from-file)",
			}},
		{"at-least per element all pass",
			ConstraintEntry{Kind: "at-least-one-of", Fields: ab},
			map[string]any{"replicas": []any{
				map[string]any{"a": int64(1)},
				map[string]any{"b": int64(2)},
			}},
			nil},
		{"at-least element none set",
			ConstraintEntry{Kind: "at-least-one-of", Fields: ab},
			map[string]any{"replicas": []any{
				map[string]any{"a": int64(1)},
				map[string]any{},
			}},
			[]string{
				"constraints[0] (at-least-one-of [var.replicas[1].a, var.replicas[1].b]): " +
					"expected at least one to be set, got none",
			}},
		{"at-most element two set",
			ConstraintEntry{Kind: "at-most-one-of", Fields: ab},
			map[string]any{"replicas": []any{
				map[string]any{"a": int64(1), "b": int64(2)},
			}},
			[]string{
				"constraints[0] (at-most-one-of [var.replicas[0].a, var.replicas[0].b]): " +
					"expected at most one to be set, got 2 (var.replicas[0].a, var.replicas[0].b)",
			}},
		{"mutually-exclusive element two set",
			ConstraintEntry{Kind: "mutually-exclusive", Fields: ab},
			map[string]any{"replicas": []any{
				map[string]any{"a": int64(1), "b": int64(2)},
			}},
			[]string{
				"constraints[0] (mutually-exclusive [var.replicas[0].a, var.replicas[0].b]): " +
					"expected at most one to be set, got 2 (var.replicas[0].a, var.replicas[0].b)",
			}},
		{"together element partial",
			ConstraintEntry{Kind: "required-together", Fields: certKey},
			map[string]any{"replicas": []any{
				map[string]any{"cert": "c", "key": "k"},
				map[string]any{"cert": "c"},
			}},
			[]string{
				"constraints[0] (required-together [var.replicas[1].cert, var.replicas[1].key]): " +
					"expected all set or all null, got 1 set (var.replicas[1].cert)",
			}},
		{"together element empty passes",
			ConstraintEntry{Kind: "required-together", Fields: certKey},
			map[string]any{"replicas": []any{
				map[string]any{"cert": "c", "key": "k"},
				map[string]any{},
			}},
			nil},
		{"with splat trigger missing global dep",
			ConstraintEntry{Kind: "required-with",
				Fields: []string{"var.replicas[*].tls", "var.ca-cert"}},
			map[string]any{"replicas": []any{
				map[string]any{"tls": true},
				map[string]any{},
			}},
			[]string{
				`constraints[0] (required-with): "var.replicas[0].tls" is set, ` +
					"so [var.ca-cert] must also be set; missing var.ca-cert",
			}},
		{"with global trigger missing splat dep",
			ConstraintEntry{Kind: "required-with",
				Fields: []string{"var.ca-cert", "var.replicas[*].tls"}},
			map[string]any{
				"ca-cert": "pem",
				"replicas": []any{
					map[string]any{"tls": true},
					map[string]any{},
				},
			},
			[]string{
				`constraints[0] (required-with): "var.ca-cert" is set, ` +
					"so [var.replicas[1].tls] must also be set; missing var.replicas[1].tls",
			}},
		{"forbidden splat trigger with global set",
			ConstraintEntry{Kind: "forbidden-with",
				Fields: []string{"var.replicas[*].insecure", "var.ca-cert"}},
			map[string]any{
				"ca-cert": "pem",
				"replicas": []any{
					map[string]any{},
					map[string]any{"insecure": true},
				},
			},
			[]string{
				`constraints[0] (forbidden-with): "var.replicas[1].insecure" is set, ` +
					"so [var.ca-cert] must be null; got var.ca-cert",
			}},
		{"root absent checks nothing",
			ConstraintEntry{Kind: "exactly-one-of", Fields: ab},
			map[string]any{},
			nil},
		{"root null checks nothing",
			ConstraintEntry{Kind: "exactly-one-of", Fields: ab},
			map[string]any{"replicas": nil},
			nil},
		{"empty list checks nothing",
			ConstraintEntry{Kind: "exactly-one-of", Fields: ab},
			map[string]any{"replicas": []any{}},
			nil},
		{"root not a list",
			ConstraintEntry{Kind: "exactly-one-of", Fields: ab},
			map[string]any{"replicas": "oops"},
			[]string{
				"constraints[0] (exactly-one-of [var.replicas[*].a, var.replicas[*].b]): " +
					"cannot splat a string at var.replicas[*]",
			}},
		{"different lists rejected",
			ConstraintEntry{Kind: "required-together",
				Fields: []string{"var.replicas[*].x", "var.volumes[*].y"}},
			map[string]any{
				"replicas": []any{map[string]any{"x": int64(1)}},
				"volumes":  []any{map[string]any{"y": int64(2)}},
			},
			[]string{
				"constraints[0] (required-together [var.replicas[*].x, var.volumes[*].y]): " +
					"[*] fields must splat the same list, got var.replicas[*] and var.volumes[*]",
			}},
		{"single splat field rejected",
			ConstraintEntry{Kind: "at-most-one-of",
				Fields: []string{"var.replicas[*].primary"}},
			map[string]any{"replicas": []any{
				map[string]any{"primary": true},
			}},
			[]string{
				"constraints[0] (at-most-one-of [var.replicas[*].primary]): " +
					"a [*] constraint needs at least two fields",
			}},
		{"nested splat per inner element",
			ConstraintEntry{Kind: "required-together",
				Fields: []string{"var.clusters[*].nodes[*].a", "var.clusters[*].nodes[*].b"}},
			map[string]any{"clusters": []any{
				map[string]any{"nodes": []any{
					map[string]any{"a": int64(1), "b": int64(2)},
				}},
				map[string]any{"nodes": []any{
					map[string]any{"a": int64(1)},
				}},
			}},
			[]string{
				"constraints[0] (required-together [var.clusters[1].nodes[0].a, " +
					"var.clusters[1].nodes[0].b]): " +
					"expected all set or all null, got 1 set (var.clusters[1].nodes[0].a)",
			}},
		{"splat root under nested map",
			ConstraintEntry{Kind: "exactly-one-of",
				Fields: []string{"var.config.replicas[*].a", "var.config.replicas[*].b"}},
			map[string]any{"config": map[string]any{
				"replicas": []any{
					map[string]any{"a": int64(1), "b": int64(2)},
				},
			}},
			[]string{
				"constraints[0] (exactly-one-of [var.config.replicas[0].a, var.config.replicas[0].b]): " +
					"expected exactly one to be set, got 2 " +
					"(var.config.replicas[0].a, var.config.replicas[0].b)",
			}},
		{"scalar elements read null",
			ConstraintEntry{Kind: "exactly-one-of",
				Fields: []string{"var.nums[*].a", "var.nums[*].b"}},
			map[string]any{"nums": []any{int64(1)}},
			[]string{
				"constraints[0] (exactly-one-of [var.nums[0].a, var.nums[0].b]): " +
					"expected exactly one to be set, got 0 ()",
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 3 {
				errs := CheckConstraintEntries([]ConstraintEntry{tt.entry}, tt.values, nil, DisplayRooted)
				var msgs []string
				for _, e := range errs.Errors() {
					msgs = append(msgs, e.Msg)
				}
				require.Equal(t, tt.wantMsgs, msgs)
			}
		})
	}
}

func TestCheckConstraintEntriesNodeRelativeDisplay(t *testing.T) {
	values := map[string]any{
		"code": map[string]any{"zip-file": "a.zip", "image-uri": "img"},
		"replicas": []any{
			map[string]any{"tls": true},
		},
	}
	tests := []struct {
		name     string
		entry    ConstraintEntry
		wantMsgs []string
	}{
		{"set kind names fields relative to the node",
			ConstraintEntry{Kind: "exactly-one-of",
				Fields: []string{"var.code.zip-file", "var.code.image-uri"}},
			[]string{
				"constraints[0] (exactly-one-of [code.zip-file, code.image-uri]): " +
					"expected exactly one to be set, got 2 (code.zip-file, code.image-uri)",
			}},
		{"trigger kinds quote relative names",
			ConstraintEntry{Kind: "required-with",
				Fields: []string{"var.code.zip-file", "var.signing-profile"}},
			[]string{
				`constraints[0] (required-with): "code.zip-file" is set, ` +
					"so [signing-profile] must also be set; missing signing-profile",
			}},
		{"splat rules name the relative list",
			ConstraintEntry{Kind: "at-most-one-of",
				Fields: []string{"var.replicas[*].primary"}},
			[]string{
				"constraints[0] (at-most-one-of [replicas[*].primary]): " +
					"a [*] constraint needs at least two fields",
			}},
		{"splat expansion names the relative element",
			ConstraintEntry{Kind: "required-together",
				Fields: []string{"var.replicas[*].tls", "var.replicas[*].cert"}},
			[]string{
				"constraints[0] (required-together [replicas[0].tls, replicas[0].cert]): " +
					"expected all set or all null, got 1 set (replicas[0].tls)",
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := CheckConstraintEntries(
				[]ConstraintEntry{tt.entry}, values, nil, DisplayNodeRelative)
			var msgs []string
			for _, e := range errs.Errors() {
				msgs = append(msgs, e.Msg)
			}
			require.Equal(t, tt.wantMsgs, msgs)
		})
	}
}

func TestLookupPath(t *testing.T) {
	values := map[string]any{
		"name": "db",
		"code": map[string]any{
			"inline":  "x",
			"signing": map[string]any{"key-arn": "arn"},
			"empty":   nil,
		},
		"listeners": []any{
			map[string]any{"cert": "c0", "key": nil},
			map[string]any{},
		},
		"matrix": []any{[]any{"a", "b"}, []any{"c"}},
		"nums":   []any{int64(1), int64(2)},
	}
	tests := []struct {
		name      string
		path      string
		wantVal   any
		wantFound bool
	}{
		{"flat present", "var.name", "db", true},
		{"flat absent", "var.region", nil, false},
		{"nested present", "var.code.inline", "x", true},
		{"present null leaf", "var.code.empty", nil, true},
		{"nested leaf absent", "var.code.missing", nil, false},
		{"nested parent absent", "var.config.inline", nil, false},
		{"step into null parent", "var.code.empty.deeper", nil, false},
		{"three-level present", "var.code.signing.key-arn", "arn", true},
		{"three-level leaf absent", "var.code.signing.profile", nil, false},
		{"step into scalar", "var.name.suffix", nil, false},
		{"indexed map field", "var.listeners[0].cert", "c0", true},
		{"indexed null leaf", "var.listeners[0].key", nil, true},
		{"indexed leaf absent", "var.listeners[1].cert", nil, false},
		{"index out of range", "var.listeners[2].cert", nil, false},
		{"trailing index", "var.listeners[1]", map[string]any{}, true},
		{"double index", "var.matrix[0][1]", "b", true},
		{"double index out of range", "var.matrix[1][1]", nil, false},
		{"index into map", "var.code[0]", nil, false},
		{"index into scalar", "var.name[0]", nil, false},
		{"index into scalar element", "var.nums[0].x", nil, false},
		{"splat is not an index", "var.listeners[*].cert", nil, false},
		{"unclosed index", "var.listeners[0.cert", nil, false},
		{"negative index", "var.listeners[-1]", nil, false},
		{"empty index", "var.listeners[]", nil, false},
		{"bare name not found", "name", nil, false},
		{"non-var root not found", "code.inline", nil, false},
		{"var alone not found", "var", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, found := lookupPath(values, tt.path)
			require.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				require.Equal(t, tt.wantVal, val)
			}
		})
	}
}

func TestParseSpecs(t *testing.T) {
	specs := []ConstraintSpec{
		{Kind: "exactly-one-of", Fields: []string{"var.a", "var.b"}},
		{Kind: "predicate", When: "var.tier == 'prod'",
			Require: "var.backups == true", Message: "m"},
	}
	entries, errs := ParseSpecs(specs)
	require.Equal(t, 0, errs.Len(), "unexpected: %v", errs.Err())
	require.Len(t, entries, 2)

	require.Equal(t, "exactly-one-of", entries[0].Kind)
	require.Equal(t, []string{"var.a", "var.b"}, entries[0].Fields)
	require.Nil(t, entries[0].When, "a set constraint has no when expression")
	require.Nil(t, entries[0].Require)

	require.Equal(t, "predicate", entries[1].Kind)
	require.NotNil(t, entries[1].When)
	require.NotNil(t, entries[1].Require)
	require.Equal(t, "m", entries[1].Message)
}

func TestParseSpecsReportsBadExpression(t *testing.T) {
	specs := []ConstraintSpec{
		{Kind: "predicate", When: "(", Require: "true"},
	}
	entries, errs := ParseSpecs(specs)
	require.Positive(t, errs.Len())
	require.Empty(t, entries, "a spec that fails to parse is skipped")
}

func TestCheckConstraintsCollectsMultiple(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: exactly-one-of, fields: [var.a, var.b] },
  { kind: required-together, fields: [var.c, var.d] },
]
`)
	errs := CheckConstraints(block, map[string]any{
		"a": nil, "b": nil,
		"c": "x", "d": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 2, errs.Len())
}
