package runtime

import (
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

func TestEvalUnknownRoot(t *testing.T) {
	_, err := Eval(parseValue(t, "weird.thing"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown address root")
}

func TestEvalCallFormat(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{
		"region": "us-east-1",
		"name":   "web",
	}}
	cases := []struct {
		src, want string
	}{
		{"format('hello')", "hello"},
		{"format('%s', 'world')", "world"},
		{"format('%s-%s', var.region, var.name)", "us-east-1-web"},
		{"format('%d items', 3)", "3 items"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), ctx)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalCallFormatNoArgs(t *testing.T) {
	_, err := Eval(parseValue(t, "format()"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "format string")
}

func TestEvalCallFormatNonStringFirst(t *testing.T) {
	_, err := Eval(parseValue(t, "format(1, 'x')"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "first argument must be a string")
}

func TestEvalCallB64Encode(t *testing.T) {
	got, err := Eval(parseValue(t, "b64-encode('hello')"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, "aGVsbG8=", got)
}

func TestEvalCallB64Decode(t *testing.T) {
	got, err := Eval(parseValue(t, "b64-decode('aGVsbG8=')"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, "hello", got)
}

func TestEvalCallB64Roundtrip(t *testing.T) {
	got, err := Eval(parseValue(t, "b64-decode(b64-encode('round trip'))"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, "round trip", got)
}

func TestEvalCallB64DecodeBad(t *testing.T) {
	_, err := Eval(parseValue(t, "b64-decode('not-base64!!')"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "b64-decode")
}

func TestEvalCallB64EncodeWrongType(t *testing.T) {
	_, err := Eval(parseValue(t, "b64-encode(1)"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a string")
}

func TestEvalCallRange(t *testing.T) {
	got, err := Eval(parseValue(t, "range(3)"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, []any{int64(0), int64(1), int64(2)}, got)

	got, err = Eval(parseValue(t, "range(0)"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, []any{}, got)
}

func TestEvalCallRangeNegative(t *testing.T) {
	_, err := Eval(parseValue(t, "range(-1)"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-negative")
}

func TestEvalCallRangeNonInt(t *testing.T) {
	_, err := Eval(parseValue(t, "range(1.5)"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "integer")
}

func TestEvalCallLength(t *testing.T) {
	cases := []struct {
		src  string
		want int64
	}{
		{"length('hello')", 5},
		{"length('')", 0},
		{"length([1, 2, 3])", 3},
		{"length([])", 0},
		{"length({ a: 1, b: 2 })", 2},
		{"length({})", 0},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), &EvalContext{})
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalCallLengthTypeError(t *testing.T) {
	_, err := Eval(parseValue(t, "length(1)"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "string, list, or map")
}

func TestEvalCallUnknown(t *testing.T) {
	_, err := Eval(parseValue(t, "frobnicate('x')"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown function")
	require.Contains(t, err.Error(), "frobnicate")
}

func TestEvalCallModuleNotSupported(t *testing.T) {
	_, err := Eval(parseValue(t, "lib.foo('x')"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "module functions are not yet supported")
}

func TestEvalCallNested(t *testing.T) {
	got, err := Eval(parseValue(t, "format('%s', b64-encode('plain'))"), &EvalContext{})
	require.NoError(t, err)
	require.Equal(t, "cGxhaW4=", got)
}

func TestEvalCallArgError(t *testing.T) {
	// An error inside an argument expression bubbles up with the call
	// name and arg index so debugging can find it.
	_, err := Eval(parseValue(t, "format('%s', var.missing)"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "format")
	require.Contains(t, err.Error(), "arg 1")
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
