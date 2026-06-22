package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
)

func splatEvalContext() *EvalContext {
	return &EvalContext{Inputs: map[string]any{
		"subnets": []any{
			map[string]any{"id": "s-1", "cidr": "10.0.0.0/24", "az": "a", "public": true},
			map[string]any{"id": "s-2", "cidr": "10.0.1.0/24", "az": "b", "public": false},
			map[string]any{"id": "s-3", "cidr": "10.0.2.0/24", "az": "a", "public": true},
		},
		"nums":   []any{int64(1), int64(2), int64(3)},
		"single": []any{map[string]any{"id": "only"}},
		"empty":  []any{},
		"grid": []any{
			[]any{map[string]any{"name": "a"}, map[string]any{"name": "b"}},
			[]any{map[string]any{"name": "c"}},
		},
		"regions": []any{
			map[string]any{"name": "east", "subnets": []any{
				map[string]any{"id": "e-1"}, map[string]any{"id": "e-2"},
			}},
			map[string]any{"name": "west", "subnets": []any{
				map[string]any{"id": "w-1"},
			}},
		},
		"servers": []any{
			map[string]any{"meta": map[string]any{"name": "web"}, "ports": []any{int64(80), int64(443)}},
			map[string]any{"meta": map[string]any{"name": "db"}, "ports": []any{int64(5432), int64(5433)}},
		},
		"matrix": []any{
			[]any{int64(1), int64(2)},
			[]any{int64(3), int64(4)},
		},
	}}
}

type splatEvalCase struct {
	name string
	src  string
	want any
}

func splatEvalCases() []splatEvalCase {
	return []splatEvalCase{
		{name: "project string field", src: "input.subnets[*].id", want: []any{"s-1", "s-2", "s-3"}},
		{
			name: "project cidr field",
			src:  "input.subnets[*].cidr",
			want: []any{"10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/24"},
		},
		{name: "project bool field", src: "input.subnets[*].public", want: []any{true, false, true}},
		{name: "project field with repeats", src: "input.subnets[*].az", want: []any{"a", "b", "a"}},
		{name: "single element list", src: "input.single[*].id", want: []any{"only"}},
		{name: "empty list projects to empty", src: "input.empty[*].id", want: []any{}},
		{
			name: "bare splat of scalars is the list",
			src:  "input.nums[*]",
			want: []any{int64(1), int64(2), int64(3)},
		},
		{
			name: "bare splat of objects is the list",
			src:  "input.single[*]",
			want: []any{map[string]any{"id": "only"}},
		},
		{
			name: "nested splat",
			src:  "input.grid[*][*].name",
			want: []any{[]any{"a", "b"}, []any{"c"}},
		},
		{
			name: "field then splat under a splat",
			src:  "input.regions[*].subnets[*].id",
			want: []any{[]any{"e-1", "e-2"}, []any{"w-1"}},
		},
		{
			name: "splat then nested object field",
			src:  "input.servers[*].meta.name",
			want: []any{"web", "db"},
		},
		{
			name: "splat then field then index",
			src:  "input.servers[*].ports[0]",
			want: []any{int64(80), int64(5432)},
		},
		{
			name: "splat then field then later index",
			src:  "input.servers[*].ports[1]",
			want: []any{int64(443), int64(5433)},
		},
		{name: "project region names", src: "input.regions[*].name", want: []any{"east", "west"}},
		{name: "index then bare splat", src: "input.matrix[0][*]", want: []any{int64(1), int64(2)}},
		{
			name: "index then bare splat other row",
			src:  "input.matrix[1][*]",
			want: []any{int64(3), int64(4)},
		},
	}
}

func TestEvalSplat(t *testing.T) {
	ctx := splatEvalContext()
	for _, c := range splatEvalCases() {
		t.Run(c.name, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), ctx)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalSplatDeterministic(t *testing.T) {
	ctx := splatEvalContext()
	for _, c := range splatEvalCases() {
		t.Run(c.name, func(t *testing.T) {
			first, err := Eval(parseValue(t, c.src), ctx)
			require.NoError(t, err)
			for range 5 {
				again, err := Eval(parseValue(t, c.src), ctx)
				require.NoError(t, err)
				require.Equal(t, first, again)
			}
		})
	}
}

func TestEvalSplatErrors(t *testing.T) {
	ctx := &EvalContext{Inputs: map[string]any{
		"region":  map[string]any{"zone": "z-1"},
		"scalar":  "hello",
		"nul":     nil,
		"subnets": []any{map[string]any{"id": "s-1"}},
		"mixed":   []any{map[string]any{"extra": "x"}, map[string]any{}},
		"notgrid": []any{[]any{int64(1)}, int64(2)},
		"servers": []any{map[string]any{"ports": []any{int64(80), int64(443)}}},
	}}
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "splat a map",
			src:  "input.region[*]",
			want: "eval: cannot splat an object at input.region[*]",
		},
		{
			name: "splat a scalar",
			src:  "input.scalar[*]",
			want: "eval: cannot splat a string at input.scalar[*]",
		},
		{name: "splat null", src: "input.nul[*]", want: "eval: cannot splat null at input.nul[*]"},
		{
			name: "missing field in element",
			src:  "input.subnets[*].bogus",
			want: "eval: input.subnets[0].bogus: not found",
		},
		{
			name: "missing field in later element",
			src:  "input.mixed[*].extra",
			want: "eval: input.mixed[1].extra: not found",
		},
		{
			name: "nested splat hits a non-list element",
			src:  "input.notgrid[*][*]",
			want: "eval: cannot splat an integer at input.notgrid[1][*]",
		},
		{
			name: "index out of range after splat",
			src:  "input.servers[*].ports[5]",
			want: "eval: input.servers[0].ports[5]: not found",
		},
		{
			name: "splat a scalar reached by a field",
			src:  "input.subnets[*].id[*]",
			want: "eval: cannot splat a string at input.subnets[0].id[*]",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Eval(parseValue(t, c.src), ctx)
			require.EqualError(t, err, c.want)
		})
	}
}

func TestEvalEachNavigatesLikeOtherRefs(t *testing.T) {
	ctx := &EvalContext{Each: map[string]lang.EachValue{"@each": {
		Key: "k",
		Value: []any{
			map[string]any{"id": "a", "ports": []any{int64(1)}},
			map[string]any{"id": "b", "ports": []any{int64(2)}},
		},
	}}}
	cases := []struct {
		name string
		src  string
		want any
	}{
		{name: "positional index then field", src: "@each.value[0].id", want: "a"},
		{name: "splat a field", src: "@each.value[*].id", want: []any{"a", "b"}},
		{
			name: "splat then field then index",
			src:  "@each.value[*].ports[0]",
			want: []any{int64(1), int64(2)},
		},
		{name: "scalar key", src: "@each.key", want: "k"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Eval(parseValue(t, c.src), ctx)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}
