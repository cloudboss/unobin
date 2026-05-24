package runtime

import (
	"errors"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

// localsCtx parses a `locals: { ... }` block and returns an EvalContext
// carrying it, seeded with the given vars and resources.
func localsCtx(t *testing.T, localsSrc string, vars, res map[string]any) *EvalContext {
	t.Helper()
	f, err := lang.ParseSource("", []byte(localsSrc+"\n"))
	require.NoError(t, err)
	return &EvalContext{
		Vars:      vars,
		Resources: res,
		Data:      map[string]any{},
		Actions:   map[string]any{},
		locals:    newLocalScope(localsBlock(f)),
	}
}

func TestEvalLocalResolves(t *testing.T) {
	cases := []struct {
		name   string
		locals string
		expr   string
		vars   map[string]any
		res    map[string]any
		want   any
	}{
		{
			name:   "string literal",
			locals: `locals: { greeting: 'hello' }`,
			expr:   "local.greeting",
			want:   "hello",
		},
		{
			name:   "number literal",
			locals: `locals: { count: 3 }`,
			expr:   "local.count",
			want:   int64(3),
		},
		{
			name:   "reads a var",
			locals: `locals: { r: var.region }`,
			expr:   "local.r",
			vars:   map[string]any{"region": "us-east-1"},
			want:   "us-east-1",
		},
		{
			name:   "boolean expression",
			locals: `locals: { is-prod: var.env == 'prod' }`,
			expr:   "local.is-prod",
			vars:   map[string]any{"env": "prod"},
			want:   true,
		},
		{
			name:   "local reads another local",
			locals: `locals: { base: var.x  derived: local.base }`,
			expr:   "local.derived",
			vars:   map[string]any{"x": "root"},
			want:   "root",
		},
		{
			name: "three local chain",
			locals: `locals: {
			  a: var.x
			  b: local.a
			  c: local.b
			}`,
			expr: "local.c",
			vars: map[string]any{"x": "deep"},
			want: "deep",
		},
		{
			name:   "declaration order does not matter",
			locals: `locals: { later: local.earlier  earlier: var.x }`,
			expr:   "local.later",
			vars:   map[string]any{"x": "ok"},
			want:   "ok",
		},
		{
			name:   "interpolation over locals and vars",
			locals: `locals: { name: $'{{var.env}}-{{local.suffix}}'  suffix: 'cluster' }`,
			expr:   "local.name",
			vars:   map[string]any{"env": "prod"},
			want:   "prod-cluster",
		},
		{
			name:   "navigate into object-valued local",
			locals: `locals: { lb: { host: 'h.example.com'  port: 443 } }`,
			expr:   "local.lb.host",
			want:   "h.example.com",
		},
		{
			name:   "reads a resource output",
			locals: `locals: { endpoint: resource.aws.lb.main.dns-name }`,
			expr:   "local.endpoint",
			res: map[string]any{"aws": map[string]any{"lb": map[string]any{
				"main": map[string]any{"dns-name": "lb-123.aws.com"},
			}}},
			want: "lb-123.aws.com",
		},
		{
			name:   "local list element",
			locals: `locals: { names: ['a', 'b', 'c'] }`,
			expr:   "local.names",
			want:   []any{"a", "b", "c"},
		},
		{
			name:   "comprehension inside a local",
			locals: `locals: { upper: [ for n in var.names : n ] }`,
			expr:   "local.upper",
			vars:   map[string]any{"names": []any{"x", "y"}},
			want:   []any{"x", "y"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := localsCtx(t, c.locals, c.vars, c.res)
			got, err := Eval(parseValue(t, c.expr), ctx)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalLocalNotFound(t *testing.T) {
	cases := []struct {
		name   string
		locals string
		expr   string
		res    map[string]any
	}{
		{
			name:   "undeclared local",
			locals: `locals: { a: 'x' }`,
			expr:   "local.missing",
		},
		{
			name:   "upstream resource not yet present",
			locals: `locals: { endpoint: resource.aws.lb.main.dns-name }`,
			expr:   "local.endpoint",
			res:    map[string]any{},
		},
		{
			name:   "chained through a missing upstream",
			locals: `locals: { a: resource.aws.lb.main.dns-name  b: local.a }`,
			expr:   "local.b",
			res:    map[string]any{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := localsCtx(t, c.locals, nil, c.res)
			_, err := Eval(parseValue(t, c.expr), ctx)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrEvalNotFound)
		})
	}
}

func TestEvalLocalCycle(t *testing.T) {
	cases := []struct {
		name   string
		locals string
		expr   string
	}{
		{
			name:   "self reference",
			locals: `locals: { a: local.a }`,
			expr:   "local.a",
		},
		{
			name:   "two-local cycle",
			locals: `locals: { a: local.b  b: local.a }`,
			expr:   "local.a",
		},
		{
			name:   "three-local cycle",
			locals: `locals: { a: local.b  b: local.c  c: local.a }`,
			expr:   "local.b",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := localsCtx(t, c.locals, nil, nil)
			_, err := Eval(parseValue(t, c.expr), ctx)
			require.Error(t, err)
			require.Contains(t, err.Error(), "cycle")
		})
	}
}

func TestEvalLocalMissingIsNotCycle(t *testing.T) {
	ctx := localsCtx(t, `locals: { endpoint: resource.aws.lb.main.dns-name }`, nil, map[string]any{})
	_, err := Eval(parseValue(t, "local.endpoint"), ctx)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "cycle")
	require.True(t, errors.Is(err, ErrEvalNotFound))
}

func TestEvalLocalNilScope(t *testing.T) {
	_, err := Eval(parseValue(t, "local.anything"), &EvalContext{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrEvalNotFound)
}

func TestEvalLocalInComprehension(t *testing.T) {
	ctx := localsCtx(t,
		`locals: { prefix: 'svc' }`,
		map[string]any{"names": []any{"a", "b"}}, nil)
	got, err := Eval(parseValue(t, "[ for n in var.names : $'{{local.prefix}}-{{n}}' ]"), ctx)
	require.NoError(t, err)
	require.Equal(t, []any{"svc-a", "svc-b"}, got)
}

func TestEvalLocalDeterministic(t *testing.T) {
	cases := []struct {
		locals string
		expr   string
		vars   map[string]any
		want   any
	}{
		{`locals: { a: var.x  b: local.a }`, "local.b", map[string]any{"x": "v"}, "v"},
		{`locals: { name: $'{{var.e}}-{{local.s}}'  s: 'c' }`, "local.name",
			map[string]any{"e": "p"}, "p-c"},
		{`locals: { obj: { k: 'val' } }`, "local.obj.k", nil, "val"},
	}
	for _, c := range cases {
		for range 20 {
			ctx := localsCtx(t, c.locals, c.vars, nil)
			got, err := Eval(parseValue(t, c.expr), ctx)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		}
	}
}
