package core

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func evalCore(t *testing.T, src string, vars map[string]any) (any, error) {
	t.Helper()
	f, err := lang.ParseSource("", []byte("v: "+src+"\n"))
	require.NoError(t, err)
	require.Len(t, f.Body.Fields, 1)
	ctx := &runtime.EvalContext{
		Vars:      vars,
		Libraries: map[string]*runtime.Library{"core": Library()},
	}
	return runtime.Eval(f.Body.Fields[0].Value, ctx)
}

func TestFunctionFormat(t *testing.T) {
	vars := map[string]any{"region": "us-east-1", "name": "web"}
	cases := []struct{ src, want string }{
		{"core.format('hello')", "hello"},
		{"core.format('%s', 'world')", "world"},
		{"core.format('%s-%s', var.region, var.name)", "us-east-1-web"},
		{"core.format('%d items', 3)", "3 items"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := evalCore(t, c.src, vars)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestFunctionFormatNonStringFirst(t *testing.T) {
	_, err := evalCore(t, "core.format(1, 'x')", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "format: argument 1 must be a string, got an integer")
}

func TestFunctionB64Encode(t *testing.T) {
	got, err := evalCore(t, "core.b64-encode('hello')", nil)
	require.NoError(t, err)
	require.Equal(t, "aGVsbG8=", got)
}

func TestFunctionB64Decode(t *testing.T) {
	got, err := evalCore(t, "core.b64-decode('aGVsbG8=')", nil)
	require.NoError(t, err)
	require.Equal(t, "hello", got)
}

func TestFunctionB64Roundtrip(t *testing.T) {
	got, err := evalCore(t, "core.b64-decode(core.b64-encode('plain text'))", nil)
	require.NoError(t, err)
	require.Equal(t, "plain text", got)
}

func TestFunctionB64DecodeBad(t *testing.T) {
	_, err := evalCore(t, "core.b64-decode('not-base64!!')", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "b64-decode")
}

func TestFunctionB64EncodeWrongType(t *testing.T) {
	_, err := evalCore(t, "core.b64-encode(1)", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a string")
}

func TestFunctionRange(t *testing.T) {
	got, err := evalCore(t, "core.range(3)", nil)
	require.NoError(t, err)
	require.Equal(t, []any{int64(0), int64(1), int64(2)}, got)

	got, err = evalCore(t, "core.range(0)", nil)
	require.NoError(t, err)
	require.Equal(t, []any{}, got)
}

func TestFunctionRangeNegative(t *testing.T) {
	_, err := evalCore(t, "core.range(-1)", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-negative")
}

func TestFunctionRangeNonInt(t *testing.T) {
	_, err := evalCore(t, "core.range(1.5)", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "integer")
}

func TestFunctionLength(t *testing.T) {
	cases := []struct {
		src  string
		want int64
	}{
		{"core.length('hello')", 5},
		{"core.length('')", 0},
		{"core.length([1, 2, 3])", 3},
		{"core.length([])", 0},
		{"core.length({ a: 1, b: 2 })", 2},
		{"core.length({})", 0},
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
	_, err := evalCore(t, "core.length(1)", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "string, list, or map")
}

func TestFunctionNested(t *testing.T) {
	got, err := evalCore(t, "core.format('%s', core.b64-encode('plain'))", nil)
	require.NoError(t, err)
	require.Equal(t, "cGxhaW4=", got)
}

func TestFunctionFormatComposites(t *testing.T) {
	cases := []struct{ src, want string }{
		{"core.format('%s', [1, 2, 3])", "[1, 2, 3]"},
		{"core.format('%s', ['a', 'b'])", "['a', 'b']"},
		{"core.format('%s', { a: 1, b: 2 })", "{ a: 1, b: 2 }"},
		{"core.format('list=%s', [])", "list=[]"},
		{"core.format('subnets=%v', ['subnet-a', 'subnet-b'])", "subnets=['subnet-a', 'subnet-b']"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := evalCore(t, c.src, nil)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestFunctionAll(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{"core.all([true, true])", true},
		{"core.all([true, false])", false},
		{"core.all([])", true},
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
		{"core.any([false, true])", true},
		{"core.any([false, false])", false},
		{"core.any([])", false},
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
	got, err := evalCore(t, "core.all([for r in var.replicas: r.port > 0])", vars)
	require.NoError(t, err)
	require.Equal(t, true, got)

	vars["replicas"] = []any{map[string]any{"port": int64(0)}}
	got, err = evalCore(t, "core.all([for r in var.replicas: r.port > 0])", vars)
	require.NoError(t, err)
	require.Equal(t, false, got)
}

func TestFunctionAllNonBooleanElement(t *testing.T) {
	_, err := evalCore(t, "core.all([true, 1])", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "all: argument 1: element 1 must be a boolean, got an integer")
}

func TestFunctionAnyNonList(t *testing.T) {
	_, err := evalCore(t, "core.any('x')", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "any: argument 1 must be a list, got a string")
}
