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

func TestEvalCallNotSupported(t *testing.T) {
	_, err := Eval(parseValue(t, "format('%s', 'x')"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported expression")
}

func TestEvalOperatorNotSupported(t *testing.T) {
	_, err := Eval(parseValue(t, "1 + 2"), &EvalContext{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported expression")
}
