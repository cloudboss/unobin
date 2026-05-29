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
  { kind: exactly-one-of, fields: [a, b, c] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"a": "x", "b": nil, "c": nil,
	}, nil)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"a": nil, "b": nil, "c": nil,
	}, nil)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "exactly-one-of")
	require.Contains(t, errs.Err().Error(), "got 0")

	errs = CheckConstraints(block, map[string]any{
		"a": "x", "b": "y", "c": nil,
	}, nil)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "got 2")
}

func TestCheckAtLeastOneOf(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: at-least-one-of, fields: [a, b] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"a": nil, "b": "x",
	}, nil)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"a": nil, "b": nil,
	}, nil)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "at-least-one-of")
}

func TestCheckAtMostOneOf(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: at-most-one-of, fields: [a, b, c] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"a": nil, "b": nil, "c": nil,
	}, nil)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"a": "x", "b": nil, "c": nil,
	}, nil)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"a": "x", "b": "y", "c": nil,
	}, nil)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "at most one")
}

func TestCheckMutuallyExclusive(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: mutually-exclusive, fields: [a, b] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"a": "x", "b": "y",
	}, nil)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "mutually-exclusive")
}

func TestCheckRequiredTogether(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: required-together, fields: [vpc-id, subnet-ids] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"vpc-id": "vpc-abc", "subnet-ids": []any{"a"},
	}, nil)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"vpc-id": nil, "subnet-ids": nil,
	}, nil)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"vpc-id": "vpc-abc", "subnet-ids": nil,
	}, nil)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "required-together")
}

func TestCheckRequiredWith(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: required-with, fields: [trigger, dep1, dep2] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"trigger": nil, "dep1": nil, "dep2": nil,
	}, nil)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"trigger": "x", "dep1": "a", "dep2": "b",
	}, nil)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"trigger": "x", "dep1": "a", "dep2": nil,
	}, nil)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "required-with")
	require.Contains(t, errs.Err().Error(), "missing dep2")
}

func TestCheckForbiddenWith(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: forbidden-with, fields: [use-spot, reserved-capacity] },
]
`)

	errs := CheckConstraints(block, map[string]any{
		"use-spot": true, "reserved-capacity": nil,
	}, nil)
	require.Equal(t, 0, errs.Len(), errs.Err())

	errs = CheckConstraints(block, map[string]any{
		"use-spot": true, "reserved-capacity": int64(10),
	}, nil)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "forbidden-with")
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
	errs := CheckConstraints(block, values, boolExprEval(values))
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
	errs := CheckConstraints(block, values, boolExprEval(values))
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
	errs := CheckConstraints(block, values, boolExprEval(values))
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
	errs := CheckConstraints(block, map[string]any{}, boolExprEval(nil))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "predicate requirement not satisfied")
}

func TestCheckConstraintEntries(t *testing.T) {
	ab := []string{"a", "b"}
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
			errs := CheckConstraintEntries([]ConstraintEntry{tt.entry}, tt.values, nil)
			if tt.wantErr {
				require.Positive(t, errs.Len())
			} else {
				require.Equal(t, 0, errs.Len(), "unexpected: %v", errs.Err())
			}
		})
	}
}

func TestCheckConstraintsCollectsMultiple(t *testing.T) {
	block := parseConstraintsBlock(t, `
constraints: [
  { kind: exactly-one-of, fields: [a, b] },
  { kind: required-together, fields: [c, d] },
]
`)
	errs := CheckConstraints(block, map[string]any{
		"a": nil, "b": nil,
		"c": "x", "d": nil,
	}, nil)
	require.Equal(t, 2, errs.Len())
}
