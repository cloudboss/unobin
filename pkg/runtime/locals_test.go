package runtime

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/ubtest"
)

func runtimeLocalsFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/locals", name)
}

// localsCtx parses a `locals: { ... }` block and returns an EvalContext
// carrying it, seeded with the given inputs and resources.
func localsCtx(t *testing.T, localsSrc string, inputs, res map[string]any) *EvalContext {
	t.Helper()
	f, err := lang.ParseSource("", []byte(localsSrc+"\n"))
	require.NoError(t, err)
	return &EvalContext{
		Inputs:    inputs,
		Resources: res,
		Data:      map[string]any{},
		Actions:   map[string]any{},
		locals:    newLocalScope(localsBlock(f)),
	}
}

func TestEvalLocalResolves(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		expr    string
		inputs  map[string]any
		res     map[string]any
		want    any
	}{
		{
			name:    "string literal",
			fixture: "local-resolves/string-literal",
			expr:    "local.greeting",
			want:    "hello",
		},
		{
			name:    "number literal",
			fixture: "local-resolves/number-literal",
			expr:    "local.count",
			want:    int64(3),
		},
		{
			name:    "reads an input",
			fixture: "local-resolves/reads-input",
			expr:    "local.r",
			inputs:  map[string]any{"region": "us-east-1"},
			want:    "us-east-1",
		},
		{
			name:    "boolean expression",
			fixture: "local-resolves/boolean-expression",
			expr:    "local.is-prod",
			inputs:  map[string]any{"env": "prod"},
			want:    true,
		},
		{
			name:    "local reads another local",
			fixture: "local-resolves/local-reads-local",
			expr:    "local.derived",
			inputs:  map[string]any{"x": "root"},
			want:    "root",
		},
		{
			name:    "three local chain",
			fixture: "local-resolves/three-local-chain",
			expr:    "local.c",
			inputs:  map[string]any{"x": "deep"},
			want:    "deep",
		},
		{
			name:    "declaration order does not matter",
			fixture: "local-resolves/declaration-order",
			expr:    "local.later",
			inputs:  map[string]any{"x": "ok"},
			want:    "ok",
		},
		{
			name:    "interpolation over locals and inputs",
			fixture: "local-resolves/interpolation",
			expr:    "local.name",
			inputs:  map[string]any{"env": "prod"},
			want:    "prod-cluster",
		},
		{
			name:    "navigate into object-valued local",
			fixture: "local-resolves/object-valued-local",
			expr:    "local.lb.host",
			want:    "h.example.com",
		},
		{
			name:    "reads a resource output",
			fixture: "local-resolves/resource-output",
			expr:    "local.endpoint",
			res: map[string]any{"aws": map[string]any{"lb": map[string]any{
				"main": map[string]any{"dns-name": "lb-123.aws.com"},
			}}},
			want: "lb-123.aws.com",
		},
		{
			name:    "local list element",
			fixture: "local-resolves/list",
			expr:    "local.names",
			want:    []any{"a", "b", "c"},
		},
		{
			name:    "comprehension inside a local",
			fixture: "local-resolves/comprehension",
			expr:    "local.upper",
			inputs:  map[string]any{"names": []any{"x", "y"}},
			want:    []any{"x", "y"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := localsCtx(t, runtimeLocalsFixture(t, c.fixture), c.inputs, c.res)
			got, err := Eval(parseValue(t, c.expr), ctx)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestEvalLocalNotFound(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		expr    string
		res     map[string]any
	}{
		{
			name:    "undeclared local",
			fixture: "not-found/undeclared-local",
			expr:    "local.missing",
		},
		{
			name:    "upstream resource not yet present",
			fixture: "not-found/upstream-resource",
			expr:    "local.endpoint",
			res:     map[string]any{},
		},
		{
			name:    "chained through a missing upstream",
			fixture: "not-found/chained-missing-upstream",
			expr:    "local.b",
			res:     map[string]any{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := localsCtx(t, runtimeLocalsFixture(t, c.fixture), nil, c.res)
			_, err := Eval(parseValue(t, c.expr), ctx)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrEvalNotFound)
		})
	}
}

func TestEvalLocalCycle(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		expr    string
	}{
		{
			name:    "self reference",
			fixture: "cycle/self-reference",
			expr:    "local.a",
		},
		{
			name:    "two-local cycle",
			fixture: "cycle/two-local-cycle",
			expr:    "local.a",
		},
		{
			name:    "three-local cycle",
			fixture: "cycle/three-local-cycle",
			expr:    "local.b",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := localsCtx(t, runtimeLocalsFixture(t, c.fixture), nil, nil)
			_, err := Eval(parseValue(t, c.expr), ctx)
			require.Error(t, err)
			require.Contains(t, err.Error(), "cycle")
		})
	}
}

func TestEvalLocalMissingIsNotCycle(t *testing.T) {
	ctx := localsCtx(t, runtimeLocalsFixture(t, "missing-is-not-cycle"), nil, map[string]any{})
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
		runtimeLocalsFixture(t, "local-in-comprehension"),
		map[string]any{"names": []any{"a", "b"}}, nil)
	got, err := Eval(parseValue(t, "[ for n in input.names : $'{{local.prefix}}-{{n}}' ]"), ctx)
	require.NoError(t, err)
	require.Equal(t, []any{"svc-a", "svc-b"}, got)
}

func TestEvalLocalDeterministic(t *testing.T) {
	cases := []struct {
		fixture string
		expr    string
		inputs  map[string]any
		want    any
	}{
		{"deterministic/local-chain", "local.b", map[string]any{"x": "v"}, "v"},
		{"deterministic/interpolation", "local.name", map[string]any{"e": "p"}, "p-c"},
		{"deterministic/object", "local.obj.k", nil, "val"},
	}
	for _, c := range cases {
		for range 20 {
			ctx := localsCtx(t, runtimeLocalsFixture(t, c.fixture), c.inputs, nil)
			got, err := Eval(parseValue(t, c.expr), ctx)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		}
	}
}

func TestNewEvalContextResolvesFileLocals(t *testing.T) {
	src := runtimeLocalsFixture(t, "new-eval-context")
	f, err := lang.ParseSource("config.ub", []byte(src))
	require.NoError(t, err)
	ctx := NewEvalContext(f)
	got, err := Eval(lang.TopLevelBlock(f, "inputs"), ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"role": "arn:aws:iam::123456789012:role/unobin-state",
		"name": "app-us-east-1",
	}, got)
}

func TestNewEvalContextNilFile(t *testing.T) {
	ctx := NewEvalContext(nil)
	_, err := Eval(parseValue(t, "local.region"), ctx)
	require.ErrorIs(t, err, ErrEvalNotFound)
	require.ErrorContains(t, err, `local "region" is not declared`)
}
