package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func subnetCtx() *EvalContext {
	return &EvalContext{Inputs: map[string]any{
		"subnets": []any{
			map[string]any{"name": "a", "az": "z1", "id": "i1", "public": true, "cidr": "10.0.0.0/24"},
			map[string]any{"name": "b", "az": "z1", "id": "i2", "public": false, "cidr": "10.0.1.0/24"},
			map[string]any{"name": "c", "az": "z2", "id": "i3", "public": true, "cidr": "10.0.2.0/24"},
		},
	}}
}

func TestEvalListComprehension(t *testing.T) {
	got, err := Eval(parseValue(t, "[ for x in input.subnets : x.cidr ]"), subnetCtx())
	require.NoError(t, err)
	require.Equal(t, []any{"10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/24"}, got)
}

func TestEvalListComprehensionBareBoundValue(t *testing.T) {
	ctx := &EvalContext{Inputs: map[string]any{"items": []any{"a", "b", "c"}}}
	got, err := Eval(parseValue(t, "[ for x in input.items : x ]"), ctx)
	require.NoError(t, err)
	require.Equal(t, []any{"a", "b", "c"}, got)
}

func TestEvalMapComprehensionIndexBy(t *testing.T) {
	got, err := Eval(parseValue(t, "{ for x in input.subnets : x.name => x }"), subnetCtx())
	require.NoError(t, err)
	m := got.(map[string]any)
	require.Len(t, m, 3)
	require.Equal(t, "z1", m["a"].(map[string]any)["az"])
	require.Equal(t, "z2", m["c"].(map[string]any)["az"])
}

func TestEvalComprehensionFilter(t *testing.T) {
	got, err := Eval(parseValue(t, "[ for x in input.subnets : x.id when x.public ]"), subnetCtx())
	require.NoError(t, err)
	require.Equal(t, []any{"i1", "i3"}, got)
}

func TestEvalComprehensionGroupBy(t *testing.T) {
	got, err := Eval(parseValue(t, "{ for x in input.subnets : x.az => x.id... }"), subnetCtx())
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"z1": []any{"i1", "i2"},
		"z2": []any{"i3"},
	}, got)
}

func TestEvalComprehensionGroupByWithFilter(t *testing.T) {
	got, err := Eval(
		parseValue(t, "{ for x in input.subnets : x.az => x.id... when x.public }"), subnetCtx())
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"z1": []any{"i1"},
		"z2": []any{"i3"},
	}, got)
}

func TestEvalComprehensionListIndexBinding(t *testing.T) {
	ctx := &EvalContext{Inputs: map[string]any{"items": []any{"a", "b", "c"}}}
	got, err := Eval(parseValue(t, "[ for i, x in input.items : i ]"), ctx)
	require.NoError(t, err)
	require.Equal(t, []any{int64(0), int64(1), int64(2)}, got)
}

func TestEvalComprehensionMapKeyValueBinding(t *testing.T) {
	ctx := &EvalContext{Inputs: map[string]any{
		"m": map[string]any{"k1": "v1", "k2": "v2"},
	}}
	got, err := Eval(parseValue(t, "{ for k, v in input.m : k => v }"), ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"k1": "v1", "k2": "v2"}, got)
}

// A map source iterates by sorted key, so the produced list is
// deterministic regardless of Go's map ordering.
func TestEvalComprehensionMapSourceSortedByKey(t *testing.T) {
	ctx := &EvalContext{Inputs: map[string]any{
		"m": map[string]any{"b": int64(2), "a": int64(1), "c": int64(3)},
	}}
	for range 20 {
		got, err := Eval(parseValue(t, "[ for v in input.m : v ]"), ctx)
		require.NoError(t, err)
		require.Equal(t, []any{int64(1), int64(2), int64(3)}, got)
	}
}

func TestEvalComprehensionConditionalBody(t *testing.T) {
	ctx := &EvalContext{Inputs: map[string]any{"n": []any{int64(1), int64(3)}}}
	got, err := Eval(parseValue(t, "[ for x in input.n : if x > 2 then 'big' else 'small' ]"), ctx)
	require.NoError(t, err)
	require.Equal(t, []any{"small", "big"}, got)
}

func TestEvalComprehensionNested(t *testing.T) {
	ctx := &EvalContext{Inputs: map[string]any{
		"nets": []any{
			map[string]any{"subnets": []any{
				map[string]any{"id": "s1"},
				map[string]any{"id": "s2"},
			}},
		},
	}}
	got, err := Eval(
		parseValue(t, "[ for net in input.nets : [ for s in net.subnets : s.id ] ]"), ctx)
	require.NoError(t, err)
	require.Equal(t, []any{[]any{"s1", "s2"}}, got)
}

func TestEvalComprehensionErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		ctx  *EvalContext
		want string
	}{
		{
			name: "duplicate key without group",
			src:  "{ for x in input.subnets : x.az => x.id }",
			ctx:  subnetCtx(),
			want: `eval: comprehension produced duplicate key "z1"; use ... to group`,
		},
		{
			name: "non-list-or-map source",
			src:  "[ for x in input.s : x ]",
			ctx:  &EvalContext{Inputs: map[string]any{"s": "scalar"}},
			want: "eval: comprehension source must be a list or map, got a string",
		},
		{
			name: "non-boolean filter",
			src:  "[ for x in input.items : x when x ]",
			ctx:  &EvalContext{Inputs: map[string]any{"items": []any{"a"}}},
			want: "eval: comprehension filter must be a boolean, got a string",
		},
		{
			name: "non-string map key",
			src:  "{ for x in input.items : x => x }",
			ctx:  &EvalContext{Inputs: map[string]any{"items": []any{int64(1)}}},
			want: "eval: comprehension key must be a string, got an integer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Eval(parseValue(t, tt.src), tt.ctx)
			require.EqualError(t, err, tt.want)
		})
	}
}
