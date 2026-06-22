package lang

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/ubtest"
)

func constraintFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/constraints", name)
}

// boolExprEval evaluates a tiny set of expressions used by the
// predicate tests: bool literals, equality and inequality between a
// reference and a literal, plus iteration binding reads under an
// iterating predicate. The real checks use runtime.Eval against an
// EvalContext seeded with the validated inputs.
func boolExprEval(values map[string]any) ConstraintEvalFunc {
	return func(e Expr, binds []EachBinding) (any, error) {
		switch v := e.(type) {
		case *BoolLit:
			return v.Value, nil
		case *DotPath:
			return evalLeaf(v, values, binds)
		case *Infix:
			left, err := evalLeaf(v.Left, values, binds)
			if err != nil {
				return nil, err
			}
			right, err := evalLeaf(v.Right, values, binds)
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

// lookupBinding finds one named iteration binding among those in scope.
func lookupBinding(binds []EachBinding, name string) (EachBinding, bool) {
	for _, b := range binds {
		if b.Name == name {
			return b, true
		}
	}
	return EachBinding{}, false
}

func evalLeaf(e Expr, values map[string]any, binds []EachBinding) (any, error) {
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
		if bound, ok := lookupBinding(binds, v.Root.Name); ok && len(v.Segments) > 0 {
			switch v.Segments[0].Name {
			case "key":
				return bound.Key, nil
			case "value":
				cur := bound.Value
				for _, seg := range v.Segments[1:] {
					m, ok := cur.(map[string]any)
					if !ok {
						return nil, nil
					}
					cur = m[seg.Name]
				}
				return cur, nil
			}
			return nil, nil
		}
		if v.Root.Name != "input" || len(v.Segments) != 1 {
			return nil, nil
		}
		return values[v.Segments[0].Name], nil
	}
	return nil, nil
}

func TestCheckExactlyOneOf(t *testing.T) {
	block := parseConstraintsBlock(t, constraintFixture(t, "check-exactly-one-of"))

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
	block := parseConstraintsBlock(t, constraintFixture(t, "check-at-least-one-of"))

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
	block := parseConstraintsBlock(t, constraintFixture(t, "check-at-most-one-of"))

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

func TestCheckRequiredTogether(t *testing.T) {
	block := parseConstraintsBlock(t, constraintFixture(t, "check-required-together"))

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
	block := parseConstraintsBlock(t, constraintFixture(t, "check-required-with"))

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
	require.Contains(t, errs.Err().Error(), "missing input.dep2")
}

func TestCheckForbiddenWith(t *testing.T) {
	block := parseConstraintsBlock(t, constraintFixture(t, "check-forbidden-with"))

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
	block := parseConstraintsBlock(t, constraintFixture(t, "check-constraints-nested-fields"))

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
	block := parseConstraintsBlock(t, constraintFixture(t, "check-constraints-splat-fields"))
	errs := CheckConstraints(block, map[string]any{
		"replicas": []any{
			map[string]any{"inline": "a"},
			map[string]any{"inline": "a", "from-file": "f"},
		},
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len(), errs.Err())
	require.Contains(t, errs.Err().Error(), "input.replicas[1].inline")
}

func TestCheckConstraintsIndexedFields(t *testing.T) {
	block := parseConstraintsBlock(t, constraintFixture(t, "check-constraints-indexed-fields"))
	errs := CheckConstraints(block, map[string]any{
		"listeners": []any{
			map[string]any{"cert": "c"},
		},
	}, nil, DisplayRooted)
	require.Equal(t, 1, errs.Len(), errs.Err())
	require.Contains(t, errs.Err().Error(), "got 1 set (input.listeners[0].cert)")
}

func TestCheckPredicateWhenFalseSkipsRequire(t *testing.T) {
	block := parseConstraintsBlock(t, constraintFixture(t, "check-predicate-when-false-skips-require"))
	values := map[string]any{
		"region":    "us-east-1",
		"fips-mode": false,
	}
	errs := CheckConstraints(block, values, boolExprEval(values), DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())
}

func TestCheckPredicateWhenTrueRequireFails(t *testing.T) {
	block := parseConstraintsBlock(t, constraintFixture(t, "check-predicate-when-true-require-fails"))
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
	block := parseConstraintsBlock(t, constraintFixture(t, "check-predicate-when-true-require-ok"))
	values := map[string]any{
		"region":    "us-gov-east-1",
		"fips-mode": true,
	}
	errs := CheckConstraints(block, values, boolExprEval(values), DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())
}

func TestCheckPredicateNoMessageDefaults(t *testing.T) {
	block := parseConstraintsBlock(t, constraintFixture(t, "check-predicate-no-message-defaults"))
	errs := CheckConstraints(block, map[string]any{}, boolExprEval(nil), DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "predicate requirement not satisfied")
}

func TestCheckConstraintEntries(t *testing.T) {
	ab := []string{"input.a", "input.b"}
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
	io := []string{"input.code.inline", "input.code.from-file"}
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
			ConstraintEntry{Kind: "required-together", Fields: []string{"input.name", "input.code.inline"}},
			map[string]any{"name": "db", "code": map[string]any{"inline": "x"}}, false},
		{"mixed flat set nested unset",
			ConstraintEntry{Kind: "required-together", Fields: []string{"input.name", "input.code.inline"}},
			map[string]any{"name": "db", "code": map[string]any{}}, true},
		{"three-level nested set",
			ConstraintEntry{Kind: "at-least-one-of", Fields: []string{"input.code.signing.key-arn"}},
			map[string]any{"code": map[string]any{
				"signing": map[string]any{"key-arn": "arn"}}}, false},
		{"three-level nested unset",
			ConstraintEntry{Kind: "at-least-one-of", Fields: []string{"input.code.signing.key-arn"}},
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
				Fields: []string{"input.listeners[0].cert", "input.listeners[0].key"}}, false},
		{"together partial",
			ConstraintEntry{Kind: "required-together",
				Fields: []string{"input.listeners[1].cert", "input.listeners[1].key"}}, true},
		{"exactly-one one set",
			ConstraintEntry{Kind: "exactly-one-of",
				Fields: []string{"input.listeners[1].cert", "input.listeners[1].key"}}, false},
		{"exactly-one out of range reads null",
			ConstraintEntry{Kind: "exactly-one-of",
				Fields: []string{"input.listeners[5].cert", "input.listeners[5].key"}}, true},
		{"together out of range all null",
			ConstraintEntry{Kind: "required-together",
				Fields: []string{"input.listeners[5].cert", "input.listeners[5].key"}}, false},
		{"at-most two set",
			ConstraintEntry{Kind: "at-most-one-of",
				Fields: []string{"input.listeners[0].cert", "input.listeners[0].key"}}, true},
		{"mixed indexed trigger with flat dep",
			ConstraintEntry{Kind: "required-with",
				Fields: []string{"input.listeners[0].cert", "input.name"}}, false},
		{"mixed indexed trigger forbids flat",
			ConstraintEntry{Kind: "forbidden-with",
				Fields: []string{"input.listeners[0].cert", "input.name"}}, true},
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
	io := []string{"input.replicas[*].inline", "input.replicas[*].from-file"}
	ab := []string{"input.replicas[*].a", "input.replicas[*].b"}
	certKey := []string{"input.replicas[*].cert", "input.replicas[*].key"}
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
				"constraints[0] (exactly-one-of [input.replicas[1].inline, input.replicas[1].from-file]): " +
					"expected exactly one to be set, got 2 " +
					"(input.replicas[1].inline, input.replicas[1].from-file)",
			}},
		{"exactly-one element neither set",
			ConstraintEntry{Kind: "exactly-one-of", Fields: io},
			map[string]any{"replicas": []any{
				map[string]any{},
				map[string]any{"inline": "a"},
			}},
			[]string{
				"constraints[0] (exactly-one-of [input.replicas[0].inline, input.replicas[0].from-file]): " +
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
				"constraints[0] (exactly-one-of [input.replicas[0].inline, input.replicas[0].from-file]): " +
					"expected exactly one to be set, got 0 ()",
				"constraints[0] (exactly-one-of [input.replicas[1].inline, input.replicas[1].from-file]): " +
					"expected exactly one to be set, got 2 " +
					"(input.replicas[1].inline, input.replicas[1].from-file)",
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
				"constraints[0] (at-least-one-of [input.replicas[1].a, input.replicas[1].b]): " +
					"expected at least one to be set, got none",
			}},
		{"at-most element two set",
			ConstraintEntry{Kind: "at-most-one-of", Fields: ab},
			map[string]any{"replicas": []any{
				map[string]any{"a": int64(1), "b": int64(2)},
			}},
			[]string{
				"constraints[0] (at-most-one-of [input.replicas[0].a, input.replicas[0].b]): " +
					"expected at most one to be set, got 2 (input.replicas[0].a, input.replicas[0].b)",
			}},
		{"at-most-one-of element two set",
			ConstraintEntry{Kind: "at-most-one-of", Fields: ab},
			map[string]any{"replicas": []any{
				map[string]any{"a": int64(1), "b": int64(2)},
			}},
			[]string{
				"constraints[0] (at-most-one-of [input.replicas[0].a, input.replicas[0].b]): " +
					"expected at most one to be set, got 2 (input.replicas[0].a, input.replicas[0].b)",
			}},
		{"together element partial",
			ConstraintEntry{Kind: "required-together", Fields: certKey},
			map[string]any{"replicas": []any{
				map[string]any{"cert": "c", "key": "k"},
				map[string]any{"cert": "c"},
			}},
			[]string{
				"constraints[0] (required-together [input.replicas[1].cert, input.replicas[1].key]): " +
					"expected all set or all null, got 1 set (input.replicas[1].cert)",
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
				Fields: []string{"input.replicas[*].tls", "input.ca-cert"}},
			map[string]any{"replicas": []any{
				map[string]any{"tls": true},
				map[string]any{},
			}},
			[]string{
				`constraints[0] (required-with): "input.replicas[0].tls" is set, ` +
					"so [input.ca-cert] must also be set; missing input.ca-cert",
			}},
		{"with global trigger missing splat dep",
			ConstraintEntry{Kind: "required-with",
				Fields: []string{"input.ca-cert", "input.replicas[*].tls"}},
			map[string]any{
				"ca-cert": "pem",
				"replicas": []any{
					map[string]any{"tls": true},
					map[string]any{},
				},
			},
			[]string{
				`constraints[0] (required-with): "input.ca-cert" is set, ` +
					"so [input.replicas[1].tls] must also be set; missing input.replicas[1].tls",
			}},
		{"forbidden splat trigger with global set",
			ConstraintEntry{Kind: "forbidden-with",
				Fields: []string{"input.replicas[*].insecure", "input.ca-cert"}},
			map[string]any{
				"ca-cert": "pem",
				"replicas": []any{
					map[string]any{},
					map[string]any{"insecure": true},
				},
			},
			[]string{
				`constraints[0] (forbidden-with): "input.replicas[1].insecure" is set, ` +
					"so [input.ca-cert] must be null; got input.ca-cert",
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
				"constraints[0] (exactly-one-of [input.replicas[*].a, input.replicas[*].b]): " +
					"cannot splat a string at input.replicas[*]",
			}},
		{"different lists rejected",
			ConstraintEntry{Kind: "required-together",
				Fields: []string{"input.replicas[*].x", "input.volumes[*].y"}},
			map[string]any{
				"replicas": []any{map[string]any{"x": int64(1)}},
				"volumes":  []any{map[string]any{"y": int64(2)}},
			},
			[]string{
				"constraints[0] (required-together [input.replicas[*].x, input.volumes[*].y]): " +
					"[*] fields must splat the same list, got input.replicas[*] and input.volumes[*]",
			}},
		{"single splat field rejected",
			ConstraintEntry{Kind: "at-most-one-of",
				Fields: []string{"input.replicas[*].primary"}},
			map[string]any{"replicas": []any{
				map[string]any{"primary": true},
			}},
			[]string{
				"constraints[0] (at-most-one-of [input.replicas[*].primary]): " +
					"a [*] constraint needs at least two fields",
			}},
		{"nested splat per inner element",
			ConstraintEntry{Kind: "required-together",
				Fields: []string{"input.clusters[*].nodes[*].a", "input.clusters[*].nodes[*].b"}},
			map[string]any{"clusters": []any{
				map[string]any{"nodes": []any{
					map[string]any{"a": int64(1), "b": int64(2)},
				}},
				map[string]any{"nodes": []any{
					map[string]any{"a": int64(1)},
				}},
			}},
			[]string{
				"constraints[0] (required-together [input.clusters[1].nodes[0].a, " +
					"input.clusters[1].nodes[0].b]): " +
					"expected all set or all null, got 1 set (input.clusters[1].nodes[0].a)",
			}},
		{"splat root under nested map",
			ConstraintEntry{Kind: "exactly-one-of",
				Fields: []string{"input.config.replicas[*].a", "input.config.replicas[*].b"}},
			map[string]any{"config": map[string]any{
				"replicas": []any{
					map[string]any{"a": int64(1), "b": int64(2)},
				},
			}},
			[]string{
				"constraints[0] (exactly-one-of [input.config.replicas[0].a, input.config.replicas[0].b]): " +
					"expected exactly one to be set, got 2 " +
					"(input.config.replicas[0].a, input.config.replicas[0].b)",
			}},
		{"scalar elements read null",
			ConstraintEntry{Kind: "exactly-one-of",
				Fields: []string{"input.nums[*].a", "input.nums[*].b"}},
			map[string]any{"nums": []any{int64(1)}},
			[]string{
				"constraints[0] (exactly-one-of [input.nums[0].a, input.nums[0].b]): " +
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
				Fields: []string{"input.code.zip-file", "input.code.image-uri"}},
			[]string{
				"constraints[0] (exactly-one-of [code.zip-file, code.image-uri]): " +
					"expected exactly one to be set, got 2 (code.zip-file, code.image-uri)",
			}},
		{"trigger kinds quote relative names",
			ConstraintEntry{Kind: "required-with",
				Fields: []string{"input.code.zip-file", "input.signing-profile"}},
			[]string{
				`constraints[0] (required-with): "code.zip-file" is set, ` +
					"so [signing-profile] must also be set; missing signing-profile",
			}},
		{"splat rules name the relative list",
			ConstraintEntry{Kind: "at-most-one-of",
				Fields: []string{"input.replicas[*].primary"}},
			[]string{
				"constraints[0] (at-most-one-of [replicas[*].primary]): " +
					"a [*] constraint needs at least two fields",
			}},
		{"splat expansion names the relative element",
			ConstraintEntry{Kind: "required-together",
				Fields: []string{"input.replicas[*].tls", "input.replicas[*].cert"}},
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
		{"flat present", "input.name", "db", true},
		{"flat absent", "input.region", nil, false},
		{"nested present", "input.code.inline", "x", true},
		{"present null leaf", "input.code.empty", nil, true},
		{"nested leaf absent", "input.code.missing", nil, false},
		{"nested parent absent", "input.config.inline", nil, false},
		{"step into null parent", "input.code.empty.deeper", nil, false},
		{"three-level present", "input.code.signing.key-arn", "arn", true},
		{"three-level leaf absent", "input.code.signing.profile", nil, false},
		{"step into scalar", "input.name.suffix", nil, false},
		{"indexed map field", "input.listeners[0].cert", "c0", true},
		{"indexed null leaf", "input.listeners[0].key", nil, true},
		{"indexed leaf absent", "input.listeners[1].cert", nil, false},
		{"index out of range", "input.listeners[2].cert", nil, false},
		{"trailing index", "input.listeners[1]", map[string]any{}, true},
		{"double index", "input.matrix[0][1]", "b", true},
		{"double index out of range", "input.matrix[1][1]", nil, false},
		{"index into map", "input.code[0]", nil, false},
		{"index into scalar", "input.name[0]", nil, false},
		{"index into scalar element", "input.nums[0].x", nil, false},
		{"splat is not an index", "input.listeners[*].cert", nil, false},
		{"unclosed index", "input.listeners[0.cert", nil, false},
		{"negative index", "input.listeners[-1]", nil, false},
		{"empty index", "input.listeners[]", nil, false},
		{"bare name not found", "name", nil, false},
		{"non-input root not found", "code.inline", nil, false},
		{"input alone not found", "input", nil, false},
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
		{Kind: "exactly-one-of", Fields: []string{"input.a", "input.b"}},
		{Kind: "predicate", When: "input.tier == 'prod'",
			Require: "input.backups == true", Message: "m"},
	}
	entries, errs := ParseSpecs(specs)
	require.Equal(t, 0, errs.Len(), "unexpected: %v", errs.Err())
	require.Len(t, entries, 2)

	require.Equal(t, "exactly-one-of", entries[0].Kind)
	require.Equal(t, []string{"input.a", "input.b"}, entries[0].Fields)
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
	block := parseConstraintsBlock(t, constraintFixture(t, "check-constraints-collects-multiple"))
	errs := CheckConstraints(block, map[string]any{
		"a": nil, "b": nil,
		"c": "x", "d": nil,
	}, nil, DisplayRooted)
	require.Equal(t, 2, errs.Len())
}

// TestCheckPredicateChainedForEach proves a chained @for-each iterates
// level under level with every binding in scope: the inner element is
// judged against the outer one, and a failure names the element as one
// path from the input down through both levels.
func TestCheckPredicateChainedForEach(t *testing.T) {
	block := parseConstraintsBlock(t, constraintFixture(t, "check-predicate-chained-for-each"))
	values := map[string]any{"items": []any{
		map[string]any{"n": "a", "subs": []any{
			map[string]any{"n": "a"},
		}},
		map[string]any{"n": "b", "subs": []any{
			map[string]any{"n": "b"},
			map[string]any{"n": "x"},
		}},
		map[string]any{"n": "c"},
	}}
	errs := CheckConstraints(block, values, boolExprEval(values), DisplayNodeRelative)
	require.Equal(t, 1, errs.Len(), errs.Err())
	require.Contains(t, errs.Errors()[0].Msg,
		"constraints[0] (predicate): sub must match its rule (items[1].subs[1])")
}

// TestCheckPredicateChainedForEachMapLevel proves a map level iterates
// in sorted key order and names the element by its key.
func TestCheckPredicateChainedForEachMapLevel(t *testing.T) {
	block := parseConstraintsBlock(t,
		constraintFixture(t, "check-predicate-chained-for-each-map-level"))
	values := map[string]any{"envs": map[string]any{
		"prod": map[string]any{"subnets": []any{
			map[string]any{"az": "set"},
			map[string]any{},
		}},
		"dev": map[string]any{},
	}}
	errs := CheckConstraints(block, values, boolExprEval(values), DisplayNodeRelative)
	require.Equal(t, 1, errs.Len(), errs.Err())
	require.Contains(t, errs.Errors()[0].Msg,
		"subnet needs an az (envs['prod'].subnets[1])")
}

// TestCheckPredicateChainedForEachNonList proves a level whose iterable
// is not a list or map reports rather than judging garbage.
func TestCheckPredicateChainedForEachNonList(t *testing.T) {
	block := parseConstraintsBlock(t,
		constraintFixture(t, "check-predicate-chained-for-each-non-list"))
	values := map[string]any{"items": []any{
		map[string]any{"subs": "not-a-list"},
	}}
	errs := CheckConstraints(block, values, boolExprEval(values), DisplayNodeRelative)
	require.Equal(t, 1, errs.Len(), errs.Err())
	require.Contains(t, errs.Errors()[0].Msg, "@for-each must iterate a list or map")
}

// TestCheckPredicateChainedForEachMalformedSkips pins the division of
// labor: a malformed chain is skipped here, with compile validation
// the place that reports it.
func TestCheckPredicateChainedForEachMalformedSkips(t *testing.T) {
	cases := []string{
		`{ kind: predicate, @for-each: [], when: true, require: false }`,
		`{ kind: predicate, @for-each: [ { @each: input.items } ], when: true, require: false }`,
		`{ kind: predicate, @for-each: [ { rule: input.items } ], when: true, require: false }`,
		`{ kind: predicate, @for-each: [ { @a: input.items }, { @a: input.items } ],` +
			` when: true, require: false }`,
		`{ kind: predicate, @for-each: [ { @a: input.items, @b: input.items } ],` +
			` when: true, require: false }`,
	}
	values := map[string]any{"items": []any{map[string]any{}}}
	for _, src := range cases {
		block := parseConstraintsBlock(t, "constraints"+": [\n  "+src+",\n]\n")
		errs := CheckConstraints(block, values, boolExprEval(values), DisplayNodeRelative)
		require.Equal(t, 0, errs.Len(), "malformed chain should skip: %s", src)
	}
}

// TestParseSpecsChainedLevels proves the embeddable level form round
// trips into a checkable entry, the path a Go-derived chained
// constraint takes through the factory.
func TestParseSpecsChainedLevels(t *testing.T) {
	specs := []ConstraintSpec{{
		Kind:    "predicate",
		When:    "true",
		Require: "@t.value.n == @rule.value.n",
		Message: "sub must match its rule",
		ForEachLevels: []ForEachSpecLevel{
			{Name: "@rule", In: "input.items"},
			{Name: "@t", In: "@rule.value.subs"},
		},
	}}
	entries, perr := ParseSpecs(specs)
	require.Equal(t, 0, perr.Len(), "specs should parse: %v", perr.Err())
	require.Len(t, entries, 1)
	require.Len(t, entries[0].Levels, 2)

	values := map[string]any{"items": []any{
		map[string]any{"n": "a", "subs": []any{map[string]any{"n": "x"}}},
	}}
	errs := CheckConstraintEntries(entries, values, boolExprEval(values), DisplayNodeRelative)
	require.Equal(t, 1, errs.Len(), errs.Err())
	require.Contains(t, errs.Err().Error(), "sub must match its rule (items[0].subs[0])")
}

func TestCheckPredicateForEachList(t *testing.T) {
	block := parseConstraintsBlock(t, constraintFixture(t, "check-predicate-for-each-list"))
	values := map[string]any{"replicas": []any{
		map[string]any{"tls": true},
		map[string]any{"tls": false},
		map[string]any{},
	}}
	errs := CheckConstraints(block, values, boolExprEval(values), DisplayRooted)
	require.Equal(t, 2, errs.Len(), errs.Err())
	require.Contains(t, errs.Errors()[0].Msg,
		"constraints[0] (predicate): tls required (input.replicas[1])")
	require.Contains(t, errs.Errors()[1].Msg,
		"constraints[0] (predicate): tls required (input.replicas[2])")

	relative := CheckConstraints(block, values, boolExprEval(values), DisplayNodeRelative)
	require.Contains(t, relative.Errors()[0].Msg, "tls required (replicas[1])")
}

func TestCheckPredicateForEachWhenGatesPerElement(t *testing.T) {
	block := parseConstraintsBlock(t,
		constraintFixture(t, "check-predicate-for-each-when-gates-per-element"))
	values := map[string]any{"replicas": []any{
		map[string]any{"enabled": true, "tls": true},
		map[string]any{"enabled": false, "tls": false},
		map[string]any{"enabled": true, "tls": false},
	}}
	errs := CheckConstraints(block, values, boolExprEval(values), DisplayRooted)
	require.Equal(t, 1, errs.Len(), errs.Err())
	require.Contains(t, errs.Errors()[0].Msg, "(input.replicas[2])")
}

func TestCheckPredicateForEachMap(t *testing.T) {
	block := parseConstraintsBlock(t, constraintFixture(t, "check-predicate-for-each-map"))
	values := map[string]any{"configs": map[string]any{
		"b": map[string]any{"on": false},
		"a": map[string]any{"on": true},
		"c": map[string]any{"on": false},
	}}
	for range 3 {
		errs := CheckConstraints(block, values, boolExprEval(values), DisplayRooted)
		require.Equal(t, 2, errs.Len(), errs.Err())
		require.Contains(t, errs.Errors()[0].Msg, "must be on (input.configs['b'])")
		require.Contains(t, errs.Errors()[1].Msg, "must be on (input.configs['c'])")
	}
}

func TestCheckPredicateForEachUnsetIterable(t *testing.T) {
	block := parseConstraintsBlock(t, constraintFixture(t, "check-predicate-for-each-unset-iterable"))
	errs := CheckConstraints(block, map[string]any{},
		boolExprEval(map[string]any{}), DisplayRooted)
	require.Equal(t, 0, errs.Len(), errs.Err())
}

func TestCheckPredicateForEachNotIterable(t *testing.T) {
	block := parseConstraintsBlock(t, constraintFixture(t, "check-predicate-for-each-not-iterable"))
	values := map[string]any{"replicas": "oops"}
	errs := CheckConstraints(block, values, boolExprEval(values), DisplayRooted)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg,
		"@for-each must iterate a list or map, got a string")
}

// rootsOfSpec parses one spec and returns its entry's field roots.
func rootsOfSpec(t *testing.T, spec ConstraintSpec) []string {
	t.Helper()
	entries, errs := ParseSpecs([]ConstraintSpec{spec})
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Len(t, entries, 1)
	return ConstraintFieldRoots(entries[0])
}

func TestConstraintFieldRoots(t *testing.T) {
	tests := []struct {
		name string
		spec ConstraintSpec
		want []string
	}{
		{
			name: "plain set fields",
			spec: ConstraintSpec{Kind: "exactly-one-of",
				Fields: []string{"input.name", "input.size"}},
			want: []string{"name", "size"},
		},
		{
			name: "nested field path keeps only the root",
			spec: ConstraintSpec{Kind: "predicate",
				Fields: []string{"input.code.inline"}},
			want: []string{"code"},
		},
		{
			name: "indexed field path keeps only the root",
			spec: ConstraintSpec{Kind: "required-together",
				Fields: []string{"input.listeners[0].cert", "input.listeners[0].key"}},
			want: []string{"listeners"},
		},
		{
			name: "splat field path keeps only the root",
			spec: ConstraintSpec{Kind: "exactly-one-of",
				Fields: []string{"input.replicas[*].a", "input.replicas[*].b"}},
			want: []string{"replicas"},
		},
		{
			name: "duplicate roots collapse",
			spec: ConstraintSpec{Kind: "at-most-one-of",
				Fields: []string{"input.code.inline", "input.code.from-file"}},
			want: []string{"code"},
		},
		{
			name: "roots come out sorted",
			spec: ConstraintSpec{Kind: "at-least-one-of",
				Fields: []string{"input.zeta", "input.alpha", "input.mid"}},
			want: []string{"alpha", "mid", "zeta"},
		},
		{
			name: "field without an input prefix is skipped",
			spec: ConstraintSpec{Kind: "exactly-one-of",
				Fields: []string{"name", "input.size"}},
			want: []string{"size"},
		},
		{
			name: "predicate when reference",
			spec: ConstraintSpec{Kind: "predicate",
				When: "input.tier == 'prod'", Require: "true"},
			want: []string{"tier"},
		},
		{
			name: "predicate require reference",
			spec: ConstraintSpec{Kind: "predicate",
				When: "true", Require: "input.size != null"},
			want: []string{"size"},
		},
		{
			name: "for-each reference",
			spec: ConstraintSpec{Kind: "predicate", ForEach: "input.items",
				When: "true", Require: "@each.value.a != null"},
			want: []string{"items"},
		},
		{
			name: "references across when, require, and for-each combine",
			spec: ConstraintSpec{Kind: "predicate", ForEach: "input.items",
				When: "input.tier == 'prod'", Require: "input.size > 0"},
			want: []string{"items", "size", "tier"},
		},
		{
			name: "each references add no roots beyond the iterable",
			spec: ConstraintSpec{Kind: "predicate", ForEach: "input.items",
				When: "@each.value.tls == true", Require: "@each.value.cert != null"},
			want: []string{"items"},
		},
		{
			name: "no references yields nothing",
			spec: ConstraintSpec{Kind: "predicate", When: "true", Require: "1 > 0"},
			want: nil,
		},
		{
			name: "reference inside a call argument",
			spec: ConstraintSpec{Kind: "predicate",
				When: "true", Require: "core.length(input.replicas) >= 2"},
			want: []string{"replicas"},
		},
		{
			name: "references on both sides of an infix",
			spec: ConstraintSpec{Kind: "predicate",
				When: "true", Require: "input.min-size <= input.max-size"},
			want: []string{"max-size", "min-size"},
		},
		{
			name: "reference inside a comprehension",
			spec: ConstraintSpec{Kind: "predicate",
				When: "true", Require: "core.all([for r in input.replicas: r.port > 0])"},
			want: []string{"replicas"},
		},
		{
			name: "comprehension binding is not a root",
			spec: ConstraintSpec{Kind: "predicate",
				When: "true", Require: "core.all([for r in input.items: r.a != input.floor])"},
			want: []string{"floor", "items"},
		},
		{
			name: "reference inside a conditional",
			spec: ConstraintSpec{Kind: "predicate",
				When: "true", Require: "if input.tier == 'prod' then input.size > 1 else true"},
			want: []string{"size", "tier"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rootsOfSpec(t, tt.spec)
			if tt.want == nil {
				require.Empty(t, got)
				return
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseSpecsRejectsUnknownKind(t *testing.T) {
	entries, errs := ParseSpecs([]ConstraintSpec{
		{Kind: "bogus-kind", Fields: []string{"input.a"}},
		{Kind: "exactly-one-of", Fields: []string{"input.a", "input.b"}},
	})
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), `unknown constraint kind "bogus-kind"`)
	require.Len(t, entries, 1)
	require.Equal(t, "exactly-one-of", entries[0].Kind)
}
