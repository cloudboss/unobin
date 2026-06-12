package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
)

func evalCore(t *testing.T, src string, vars map[string]any) (any, error) {
	t.Helper()
	f, err := lang.ParseSource("", []byte("v: "+src+"\n"))
	require.NoError(t, err)
	require.Len(t, f.Body.Fields, 1)
	return Eval(f.Body.Fields[0].Value, &EvalContext{Vars: vars})
}

func TestFunctionB64Encode(t *testing.T) {
	got, err := evalCore(t, "@core.b64-encode('hello')", nil)
	require.NoError(t, err)
	require.Equal(t, "aGVsbG8=", got)
}

func TestFunctionB64Decode(t *testing.T) {
	got, err := evalCore(t, "@core.b64-decode('aGVsbG8=')", nil)
	require.NoError(t, err)
	require.Equal(t, "hello", got)
}

func TestFunctionB64Roundtrip(t *testing.T) {
	got, err := evalCore(t, "@core.b64-decode(@core.b64-encode('plain text'))", nil)
	require.NoError(t, err)
	require.Equal(t, "plain text", got)
}

func TestFunctionB64DecodeBad(t *testing.T) {
	_, err := evalCore(t, "@core.b64-decode('not-base64!!')", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "b64-decode")
}

func TestFunctionB64EncodeWrongType(t *testing.T) {
	_, err := evalCore(t, "@core.b64-encode(1)", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a string")
}

func TestFunctionRange(t *testing.T) {
	got, err := evalCore(t, "@core.range(3)", nil)
	require.NoError(t, err)
	require.Equal(t, []any{int64(0), int64(1), int64(2)}, got)

	got, err = evalCore(t, "@core.range(0)", nil)
	require.NoError(t, err)
	require.Equal(t, []any{}, got)
}

func TestFunctionRangeNegative(t *testing.T) {
	_, err := evalCore(t, "@core.range(-1)", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-negative")
}

func TestFunctionRangeNonInt(t *testing.T) {
	_, err := evalCore(t, "@core.range(1.5)", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "integer")
}

func TestFunctionLength(t *testing.T) {
	cases := []struct {
		src  string
		want int64
	}{
		{"@core.length('hello')", 5},
		{"@core.length('')", 0},
		{"@core.length([1, 2, 3])", 3},
		{"@core.length([])", 0},
		{"@core.length({ a: 1, b: 2 })", 2},
		{"@core.length({})", 0},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := evalCore(t, c.src, nil)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestFunctionLengthTypeError(t *testing.T) {
	_, err := evalCore(t, "@core.length(1)", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "string, list, or map")
}

func TestFunctionLengthCountsRunes(t *testing.T) {
	// Characters that take more than one byte each: length counts
	// characters, not bytes.
	cases := []struct {
		src  string
		want int64
	}{
		{"@core.length('café')", 4},
		{"@core.length('日本語')", 3},
		{"@core.length('😀')", 1},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := evalCore(t, c.src, nil)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestFunctionNested(t *testing.T) {
	got, err := evalCore(t, "@core.join(['x', @core.b64-encode('plain')], '-')", nil)
	require.NoError(t, err)
	require.Equal(t, "x-cGxhaW4=", got)
}

func TestFunctionJoin(t *testing.T) {
	vars := map[string]any{
		"hosts": []any{"web-1", "web-2", "web-3"},
		"ports": []any{int64(80), int64(443)},
	}
	cases := []struct{ src, want string }{
		{"@core.join(['a', 'b', 'c'], ', ')", "a, b, c"},
		{"@core.join(['only'], ', ')", "only"},
		{"@core.join([], ', ')", ""},
		{"@core.join(['a', 'b'], '')", "ab"},
		{"@core.join(['a', 'b'], ' -> ')", "a -> b"},
		{"@core.join([1, 2, 3], '-')", "1-2-3"},
		{"@core.join([true, false], ' ')", "true false"},
		{"@core.join([1.5, 2.0], ',')", "1.5,2"},
		{"@core.join(['a', 1, true], ' ')", "a 1 true"},
		{"@core.join(var.hosts, ', ')", "web-1, web-2, web-3"},
		{"@core.join(var.ports, ':')", "80:443"},
		{"@core.join(@core.range(3), '+')", "0+1+2"},
		{"@core.join([for h in var.hosts: h when h != 'web-2'], '/')", "web-1/web-3"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := evalCore(t, c.src, vars)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

// TestFunctionJoinMatchesSlotRendering proves a joined element and an
// interpolation slot turn the same scalar into the same text.
func TestFunctionJoinMatchesSlotRendering(t *testing.T) {
	scalars := []any{"text", true, false, int64(42), 1.25, 2.0}
	for _, v := range scalars {
		vars := map[string]any{"x": v}
		joined, err := evalCore(t, "@core.join([var.x], '')", vars)
		require.NoError(t, err)
		slotted, err := evalCore(t, "$'{{ var.x }}'", vars)
		require.NoError(t, err)
		require.Equal(t, slotted, joined)
	}
}

func TestFunctionJoinNullElement(t *testing.T) {
	_, err := evalCore(t, "@core.join(['a', null, 'c'], ',')", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "join: element 1 is null")
}

func TestFunctionJoinCompositeElement(t *testing.T) {
	vars := map[string]any{"xs": []any{"a", []any{"b"}}}
	_, err := evalCore(t, "@core.join(var.xs, ',')", vars)
	require.Error(t, err)
	require.Contains(t, err.Error(), "join: element 1 must be a scalar, got a list")
}

func TestFunctionJoinNonListFirst(t *testing.T) {
	_, err := evalCore(t, "@core.join('a', ',')", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "join: argument 1 must be a list, got a string")
}

func TestFunctionJoinNonStringSeparator(t *testing.T) {
	_, err := evalCore(t, "@core.join(['a'], 1)", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "join: argument 2 must be a string, got an integer")
}

func TestFunctionToJSON(t *testing.T) {
	vars := map[string]any{"missing": nil}
	cases := []struct{ src, want string }{
		{"@core.to-json('hi')", `"hi"`},
		{`@core.to-json('with \'quote\'')`, `"with 'quote'"`},
		{`@core.to-json('a\nb')`, `"a\nb"`},
		{"@core.to-json('a<b>&c')", `"a<b>&c"`},
		{"@core.to-json(1)", "1"},
		{"@core.to-json(1.5)", "1.5"},
		{"@core.to-json(true)", "true"},
		{"@core.to-json(null)", "null"},
		{"@core.to-json(var.missing)", "null"},
		{"@core.to-json([])", "[]"},
		{"@core.to-json(['a', 'b'])", `["a","b"]`},
		{"@core.to-json([1, [2, 3]])", "[1,[2,3]]"},
		{"@core.to-json({})", "{}"},
		{"@core.to-json({ b: 2, a: 1 })", `{"a":1,"b":2}`},
		{"@core.to-json({ a: [1, 2], b: { c: true } })", `{"a":[1,2],"b":{"c":true}}`},
		{"@core.to-json({ note: null })", `{"note":null}`},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := evalCore(t, c.src, vars)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

// TestFunctionToJSONDeterministic proves object keys come out sorted,
// so repeated rendering of the same value is byte-identical.
func TestFunctionToJSONDeterministic(t *testing.T) {
	src := "@core.to-json({ e: 5, a: 1, d: { z: 26, m: 13 }, c: [3], b: 2 })"
	want := `{"a":1,"b":2,"c":[3],"d":{"m":13,"z":26},"e":5}`
	for range 20 {
		got, err := evalCore(t, src, nil)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestFunctionAll(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{"@core.all([true, true])", true},
		{"@core.all([true, false])", false},
		{"@core.all([])", true},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := evalCore(t, c.src, nil)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestFunctionAny(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{"@core.any([false, true])", true},
		{"@core.any([false, false])", false},
		{"@core.any([])", false},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := evalCore(t, c.src, nil)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestFunctionAllOverComprehension(t *testing.T) {
	vars := map[string]any{"replicas": []any{
		map[string]any{"port": int64(443)},
		map[string]any{"port": int64(8080)},
	}}
	got, err := evalCore(t, "@core.all([for r in var.replicas: r.port > 0])", vars)
	require.NoError(t, err)
	require.Equal(t, true, got)

	vars["replicas"] = []any{map[string]any{"port": int64(0)}}
	got, err = evalCore(t, "@core.all([for r in var.replicas: r.port > 0])", vars)
	require.NoError(t, err)
	require.Equal(t, false, got)
}

func TestFunctionAllNonBooleanElement(t *testing.T) {
	_, err := evalCore(t, "@core.all([true, 1])", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "all: argument 1: element 1 must be a boolean, got an integer")
}

func TestFunctionAnyNonList(t *testing.T) {
	_, err := evalCore(t, "@core.any('x')", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "any: argument 1 must be a list, got a string")
}

func TestFunctionToInteger(t *testing.T) {
	cases := []struct {
		src  string
		want int64
	}{
		{"@core.to-integer(5)", 5},
		{"@core.to-integer(2.9)", 2},
		{"@core.to-integer(-2.9)", -2},
		{"@core.to-integer(2.0)", 2},
		{"@core.to-integer('42')", 42},
		{"@core.to-integer('-7')", -7},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := evalCore(t, c.src, nil)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestFunctionToIntegerErrors(t *testing.T) {
	cases := []struct{ src, msg string }{
		{"@core.to-integer('abc')", `to-integer: "abc" is not an integer`},
		{"@core.to-integer('2.5')", `to-integer: "2.5" is not an integer`},
		{"@core.to-integer(true)", "to-integer: argument must be a string or number, got a boolean"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			_, err := evalCore(t, c.src, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), c.msg)
		})
	}
}

func TestFunctionToNumber(t *testing.T) {
	cases := []struct {
		src  string
		want float64
	}{
		{"@core.to-number(5)", 5.0},
		{"@core.to-number(1.5)", 1.5},
		{"@core.to-number('3.14')", 3.14},
		{"@core.to-number('42')", 42.0},
		{"@core.to-number('-0.5')", -0.5},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := evalCore(t, c.src, nil)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestFunctionToNumberErrors(t *testing.T) {
	cases := []struct{ src, msg string }{
		{"@core.to-number('abc')", `to-number: "abc" is not a number`},
		{"@core.to-number(true)", "to-number: argument must be a string or number, got a boolean"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			_, err := evalCore(t, c.src, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), c.msg)
		})
	}
}

func TestFunctionToString(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"@core.to-string('hi')", "hi"},
		{"@core.to-string(42)", "42"},
		{"@core.to-string(1.5)", "1.5"},
		{"@core.to-string(2.0)", "2"},
		{"@core.to-string(true)", "true"},
		{"@core.to-string(false)", "false"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := evalCore(t, c.src, nil)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestFunctionToStringErrors(t *testing.T) {
	cases := []struct{ src, msg string }{
		{"@core.to-string([1])", "to-string: argument must be a string, number, or boolean, got a list"},
		{
			"@core.to-string({ a: 1 })",
			"to-string: argument must be a string, number, or boolean, got an object",
		},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			_, err := evalCore(t, c.src, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), c.msg)
		})
	}
}

func TestFunctionToBoolean(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"@core.to-boolean('true')", true},
		{"@core.to-boolean('false')", false},
		{"@core.to-boolean(true)", true},
		{"@core.to-boolean(false)", false},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := evalCore(t, c.src, nil)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestFunctionToBooleanErrors(t *testing.T) {
	cases := []struct{ src, msg string }{
		{"@core.to-boolean('yes')", `to-boolean: "yes" is not true or false`},
		{"@core.to-boolean('True')", `to-boolean: "True" is not true or false`},
		{"@core.to-boolean(1)", "to-boolean: argument must be a string or boolean, got an integer"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			_, err := evalCore(t, c.src, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), c.msg)
		})
	}
}
