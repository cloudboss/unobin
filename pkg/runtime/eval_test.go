package runtime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func parseValue(t *testing.T, src string) lang.Expr {
	t.Helper()
	f, err := lang.ParseSource("", []byte("v: "+src+"\n"))
	require.NoError(t, err)
	require.Len(t, f.Body.Fields, 1)
	return f.Body.Fields[0].Value
}

func TestEvalLiterals(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{"'hello'", "hello"},
		{"42", int64(42)},
		{"3.14", 3.14},
		{"true", true},
		{"false", false},
		{"null", nil},
		{"some-ident", "some-ident"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), &EvalContext{})
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalArray(t *testing.T) {
	got, err := Eval(parseValue(t, "[1, 'two', true]"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, []any{int64(1), "two", true}, got)
}

func TestEvalObject(t *testing.T) {
	got, err := Eval(parseValue(t, "{ name: 'web', size: 3 }"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"name": "web", "size": int64(3)}, got)
}

func TestEvalNestedArrayInObject(t *testing.T) {
	got, err := Eval(parseValue(t, "{ argv: ['echo', 'hi'] }"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"argv": []any{"echo", "hi"}}, got)
}

func TestEvalVarSimple(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{"region": "us-east-1"}}
	got, err := Eval(parseValue(t, "var.region"), ctx)
	require.NoError(t, err)
	require.Equal(t, "us-east-1", got)
}

func TestEvalVarNested(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{
		"network": map[string]any{
			"vpc-id": "vpc-abc",
			"subnets": map[string]any{
				"public": "subnet-1",
			},
		},
	}}
	got, err := Eval(parseValue(t, "var.network.vpc-id"), ctx)
	require.NoError(t, err)
	require.Equal(t, "vpc-abc", got)

	got, err = Eval(parseValue(t, "var.network.subnets.public"), ctx)
	require.NoError(t, err)
	require.Equal(t, "subnet-1", got)
}

func TestEvalVarMissingKey(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{"region": "us-east-1"}}
	_, err := Eval(parseValue(t, "var.missing"), ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestEvalVarReferencedInArray(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{"greeting": "world"}}
	got, err := Eval(parseValue(t, "['echo', var.greeting]"), ctx)
	require.NoError(t, err)
	require.Equal(t, []any{"echo", "world"}, got)
}

func TestEvalVarReferencedInObject(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{
		"region": "us-east-1",
		"size":   int64(3),
	}}
	got, err := Eval(parseValue(t, "{ region: var.region, size: var.size }"), ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"region": "us-east-1",
		"size":   int64(3),
	}, got)
}

func TestEvalIndexedAddress(t *testing.T) {
	ctx := &EvalContext{Resources: map[string]any{
		"aws": map[string]any{
			"instance": map[string]any{
				"nodes": map[string]any{
					"alpha": map[string]any{"id": "i-abc"},
				},
			},
		},
	}}
	got, err := Eval(parseValue(t, "resource.aws.instance.nodes['alpha'].id"), ctx)
	require.NoError(t, err)
	require.Equal(t, "i-abc", got)
}

func TestEvalPositionalIndex(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{
		"names": []any{"alpha", "beta", "gamma"},
		"subnets": []any{
			map[string]any{"id": "s-1", "az": "a"},
			map[string]any{"id": "s-2", "az": "b"},
		},
		"matrix": []any{
			[]any{int64(1), int64(2)},
			[]any{int64(3), int64(4)},
		},
		"region": map[string]any{"zones": []any{"z-1", "z-2"}},
		"regions": []any{
			map[string]any{"subnets": []any{
				map[string]any{"id": "s-1-0"},
				map[string]any{"id": "s-1-1"},
			}},
		},
		"grid": []any{
			[]any{map[string]any{"name": "a"}, map[string]any{"name": "b"}},
			[]any{map[string]any{"name": "c"}, map[string]any{"name": "d"}},
		},
		"i": int64(1),
	}}
	cases := []struct {
		name string
		src  string
		want any
	}{
		{name: "first element", src: "var.names[0]", want: "alpha"},
		{name: "last element", src: "var.names[2]", want: "gamma"},
		{name: "index then field", src: "var.subnets[0].id", want: "s-1"},
		{name: "field after later element", src: "var.subnets[1].az", want: "b"},
		{name: "nested list", src: "var.matrix[0][1]", want: int64(2)},
		{name: "nested list other", src: "var.matrix[1][0]", want: int64(3)},
		{name: "computed index", src: "var.names[var.i]", want: "beta"},
		{name: "arithmetic index", src: "var.names[1 + 1]", want: "gamma"},
		{name: "map field then index", src: "var.region.zones[1]", want: "z-2"},
		{name: "deep alternating list and map", src: "var.regions[0].subnets[1].id", want: "s-1-1"},
		{name: "list of lists of maps", src: "var.grid[1][0].name", want: "c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), ctx)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalNavigateErrors(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{
		"names":    []any{"alpha", "beta"},
		"region":   map[string]any{"zone": "z-1"},
		"empty":    []any{},
		"emptymap": map[string]any{},
		"scalar":   "hello",
		"sparse":   []any{nil},
	}}
	cases := []struct {
		name string
		src  string
		want string
	}{
		{name: "out of range", src: "var.names[5]", want: "not found"},
		{name: "negative index", src: "var.names[-1]", want: "not found"},
		{name: "empty list", src: "var.empty[0]", want: "not found"},
		{name: "empty map field", src: "var.emptymap.missing", want: "not found"},
		{name: "empty map string key", src: "var.emptymap['missing']", want: "not found"},
		{name: "integer index into map", src: "var.region[0]", want: "cannot index into"},
		{name: "index into scalar", src: "var.scalar[0]", want: "cannot index into"},
		{name: "string key into list", src: "var.names['x']", want: "cannot navigate into"},
		{name: "field into list", src: "var.names.field", want: "cannot navigate into"},
		{name: "null element then field", src: "var.sparse[0].x", want: "cannot navigate into"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Eval(parseValue(t, c.src), ctx)
			require.Error(t, err)
			require.Contains(t, err.Error(), c.want)
		})
	}
}

func TestEvalConditional(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{"prod": true, "n": int64(5)}}
	tests := []struct {
		name string
		src  string
		want any
	}{
		{name: "true takes then", src: "if true then 'a' else 'b'", want: "a"},
		{name: "false takes else", src: "if false then 'a' else 'b'", want: "b"},
		{name: "var condition", src: "if var.prod then 'big' else 'small'", want: "big"},
		{name: "comparison condition", src: "if var.n > 3 then 'hi' else 'lo'", want: "hi"},
		{name: "else-if chain", src: "if false then 1 else if true then 2 else 3", want: int64(2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(parseValue(t, tt.src), ctx)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEvalConditionalShortCircuits(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{}}
	// The dead branch reads a missing var, but must not be evaluated.
	got, err := Eval(parseValue(t, "if true then 'ok' else var.missing"), ctx)
	require.NoError(t, err)
	require.Equal(t, "ok", got)

	got, err = Eval(parseValue(t, "if false then var.missing else 'ok'"), ctx)
	require.NoError(t, err)
	require.Equal(t, "ok", got)
}

func TestEvalConditionalNonBoolCondition(t *testing.T) {
	_, err := Eval(parseValue(t, "if 'yes' then 1 else 0"), &EvalContext{})
	require.Error(t, err)
}

func TestEvalUnknownRoot(t *testing.T) {
	_, err := Eval(parseValue(t, "weird.thing"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown address root")
}

func TestEvalCallBareRejected(t *testing.T) {
	_, err := Eval(parseValue(t, "frobnicate('x')"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be qualified")
	require.Contains(t, err.Error(), "frobnicate")
}

func TestEvalCallArgError(t *testing.T) {
	// An error inside an argument expression bubbles up with the call
	// name and arg index so debugging can find it.
	ctx := &EvalContext{Libraries: map[string]*Library{
		"lib": {
			Name: "lib",
			Functions: map[string]FunctionType{
				"upper": {Name: "upper", Func: func(args []any) (any, error) {
					return args[0], nil
				}},
			},
		},
	}}
	_, err := Eval(parseValue(t, "lib.upper(var.missing)"), ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lib.upper")
	require.Contains(t, err.Error(), "arg 0")
}

func TestEvalArithmeticInt(t *testing.T) {
	cases := []struct {
		src  string
		want int64
	}{
		{"1 + 2", 3},
		{"5 - 3", 2},
		{"4 * 6", 24},
		{"10 / 2", 5},
		{"7 / 2", 3},
		{"1 + 2 + 3", 6},
		{"2 + 3 * 4", 14},
		{"(1 + 2) * 3", 9},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), &EvalContext{})
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalArithmeticFloat(t *testing.T) {
	cases := []struct {
		src  string
		want float64
	}{
		{"1.5 + 2.5", 4.0},
		{"1.5 - 0.5", 1.0},
		{"2.0 * 3.0", 6.0},
		{"7.0 / 2.0", 3.5},
		{"1 + 2.5", 3.5},
		{"5.0 / 2", 2.5},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), &EvalContext{})
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalArithmeticNegative(t *testing.T) {
	cases := []struct {
		src  string
		want int64
	}{
		{"1 - 5", -4},
		{"-7 / 2", -3},
		{"-1 - -1", 0},
		{"-3 + 1", -2},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), &EvalContext{})
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalArithmeticBigInt(t *testing.T) {
	// 2^53 + 1: exact in int64, loses a bit when round-tripped through
	// float64. The arithmetic path must stay in the integer domain.
	ctx := &EvalContext{Vars: map[string]any{
		"big": int64(1<<53 + 1),
	}}
	got, err := Eval(parseValue(t, "var.big + 0"), ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1<<53+1), got)
}

func TestEvalArithmeticDivByZero(t *testing.T) {
	_, err := Eval(parseValue(t, "1 / 0"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "division by zero")

	_, err = Eval(parseValue(t, "1.0 / 0.0"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "division by zero")
}

func TestEvalArithmeticTypeError(t *testing.T) {
	_, err := Eval(parseValue(t, "'a' + 1"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be numbers")
}

func TestEvalComparisonNumeric(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"1 < 2", true},
		{"2 < 1", false},
		{"1 <= 1", true},
		{"3 > 2", true},
		{"2 >= 3", false},
		{"1 < 1.5", true},
		{"1.5 >= 1", true},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), &EvalContext{})
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalComparisonString(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"'a' < 'b'", true},
		{"'a' >= 'a'", true},
		{"'b' < 'a'", false},
		{"'aa' > 'a'", true},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), &EvalContext{})
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalComparisonTypeError(t *testing.T) {
	_, err := Eval(parseValue(t, "1 < 'a'"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "numbers or strings")
}

func TestEvalEquality(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"1 == 1", true},
		{"1 != 2", true},
		{"1 == 1.0", true},
		{"1.0 == 1", true},
		{"'a' == 'a'", true},
		{"'a' != 'b'", true},
		{"true == true", true},
		{"true == false", false},
		{"null == null", true},
		{"null == 1", false},
		{"1 == 'a'", false},
		{"[1, 2] == [1, 2]", true},
		{"[1, 2] != [1, 3]", true},
		{"{ a: 1 } == { a: 1 }", true},
		{"{ a: 1 } != { a: 2 }", true},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), &EvalContext{})
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalLogical(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"true && true", true},
		{"true && false", false},
		{"false || true", true},
		{"false || false", false},
		{"true || true", true},
		{"true && true && true", true},
		{"false || false || true", true},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), &EvalContext{})
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalLogicalShortCircuit(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{}}

	got, err := Eval(parseValue(t, "false && var.missing"), ctx)
	require.NoError(t, err)
	require.Equal(t, false, got)

	got, err = Eval(parseValue(t, "true || var.missing"), ctx)
	require.NoError(t, err)
	require.Equal(t, true, got)
}

func TestEvalLogicalTypeError(t *testing.T) {
	_, err := Eval(parseValue(t, "1 && true"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "boolean")

	_, err = Eval(parseValue(t, "true && 1"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "boolean")
}

func TestEvalPrefixNeg(t *testing.T) {
	got, err := Eval(parseValue(t, "-5"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, int64(-5), got)

	got, err = Eval(parseValue(t, "-1.5"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, -1.5, got)

	ctx := &EvalContext{Vars: map[string]any{"x": int64(3), "y": int64(4)}}
	got, err = Eval(parseValue(t, "-(var.x + var.y)"), ctx)
	require.NoError(t, err)
	require.Equal(t, int64(-7), got)
}

func TestEvalPrefixNot(t *testing.T) {
	got, err := Eval(parseValue(t, "!true"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, false, got)

	got, err = Eval(parseValue(t, "!false"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, true, got)
}

func TestEvalPrefixDoubleNot(t *testing.T) {
	got, err := Eval(parseValue(t, "!!true"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, true, got)

	got, err = Eval(parseValue(t, "!!false"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, false, got)
}

func TestEvalEqualityCollectionNoPromote(t *testing.T) {
	// Element-wise numeric promotion is intentionally NOT done for
	// collections; pinning the behavior so a future change is a
	// deliberate choice, not a silent shift.
	got, err := Eval(parseValue(t, "[1] == [1.0]"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, false, got)

	got, err = Eval(parseValue(t, "{ a: 1 } == { a: 1.0 }"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, false, got)
}

func TestEvalPrefixTypeError(t *testing.T) {
	_, err := Eval(parseValue(t, "!1"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "boolean")

	_, err = Eval(parseValue(t, "-'a'"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "number")
}

func TestEvalEachKey(t *testing.T) {
	ctx := &EvalContext{EachKey: "alpha", EachValue: "v", ForEach: true}
	got, err := Eval(parseValue(t, "@each.key"), ctx)
	require.NoError(t, err)
	require.Equal(t, "alpha", got)
}

func TestEvalEachValueScalar(t *testing.T) {
	ctx := &EvalContext{EachKey: "alpha", EachValue: "v", ForEach: true}
	got, err := Eval(parseValue(t, "@each.value"), ctx)
	require.NoError(t, err)
	require.Equal(t, "v", got)
}

func TestEvalEachValueNested(t *testing.T) {
	ctx := &EvalContext{
		EachKey:   "alpha",
		EachValue: map[string]any{"size": int64(3), "subnet": "s-1"},
		ForEach:   true,
	}
	got, err := Eval(parseValue(t, "@each.value.size"), ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), got)

	got, err = Eval(parseValue(t, "@each.value.subnet"), ctx)
	require.NoError(t, err)
	require.Equal(t, "s-1", got)
}

func TestEvalEachOutsideForEachIsError(t *testing.T) {
	_, err := Eval(parseValue(t, "@each.key"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "@each")
}

func TestEvalCallModuleFunction(t *testing.T) {
	ctx := &EvalContext{
		Vars: map[string]any{"name": "web"},
		Libraries: map[string]*Library{
			"lib": {
				Name: "lib",
				Functions: map[string]FunctionType{
					"upper": {
						Name: "upper",
						Func: func(args []any) (any, error) {
							return strings.ToUpper(args[0].(string)), nil
						},
					},
				},
			},
		},
	}
	got, err := Eval(parseValue(t, "lib.upper(var.name)"), ctx)
	require.NoError(t, err)
	require.Equal(t, "WEB", got)
}

func TestEvalCallModuleNotImported(t *testing.T) {
	_, err := Eval(parseValue(t, "lib.upper('x')"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), `"lib"`)
}

func TestEvalCallModuleFunctionNotFound(t *testing.T) {
	ctx := &EvalContext{
		Libraries: map[string]*Library{
			"lib": {Name: "lib"},
		},
	}
	_, err := Eval(parseValue(t, "lib.upper('x')"), ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), `"upper"`)
}

func TestEvalCallModuleFunctionError(t *testing.T) {
	ctx := &EvalContext{
		Libraries: map[string]*Library{
			"lib": {
				Name: "lib",
				Functions: map[string]FunctionType{
					"boom": {
						Name: "boom",
						Func: func(args []any) (any, error) {
							return nil, fmt.Errorf("kaboom")
						},
					},
				},
			},
		},
	}
	_, err := Eval(parseValue(t, "lib.boom()"), ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "kaboom")
}

func TestEvalNestedExpr(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{
		"size":   int64(3),
		"region": "us-east-1",
	}}
	got, err := Eval(parseValue(t, "(var.size + 1) * 2"), ctx)
	require.NoError(t, err)
	require.Equal(t, int64(8), got)

	got, err = Eval(parseValue(t, "var.size > 0 && var.size < 10"), ctx)
	require.NoError(t, err)
	require.Equal(t, true, got)

	got, err = Eval(parseValue(t,
		"var.region == 'us-east-1' || var.region == 'us-west-2'"), ctx)
	require.NoError(t, err)
	require.Equal(t, true, got)
}
