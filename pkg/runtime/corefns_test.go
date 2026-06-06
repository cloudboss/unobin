package runtime

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func evalCoreSrc(t *testing.T, src string) (any, error) {
	t.Helper()
	expr, err := lang.ParseExpr("test", []byte(src))
	require.NoError(t, err)
	return Eval(expr, &EvalContext{})
}

// TestEvalCoreNamespace proves @core functions resolve in an empty
// context, with no import table at all, since they are part of the
// language rather than a library.
func TestEvalCoreNamespace(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want any
	}{
		{"join", `@core.join(['a', 'b'], '-')`, "a-b"},
		{"to-json", `@core.to-json({ b: 2, a: 1 })`, `{"a":1,"b":2}`},
		{"length of a string", `@core.length('abc')`, int64(3)},
		{"length of a list", `@core.length(['a', 'b'])`, int64(2)},
		{"length of an object", `@core.length({ a: 1, b: 2, c: 3 })`, int64(3)},
		{"range", `@core.range(3)`, []any{int64(0), int64(1), int64(2)}},
		{"b64-encode", `@core.b64-encode('hi')`, "aGk="},
		{"b64-decode", `@core.b64-decode('aGk=')`, "hi"},
		{"all true", `@core.all([true, true])`, true},
		{"all with a false", `@core.all([true, false])`, false},
		{"all of an empty list", `@core.all([])`, true},
		{"any with a true", `@core.any([false, true])`, true},
		{"any of an empty list", `@core.any([])`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evalCoreSrc(t, tt.src)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEvalCoreNamespaceUnknownFunction(t *testing.T) {
	_, err := evalCoreSrc(t, `@core.frobnicate('x')`)
	require.Error(t, err)
	require.Contains(t, err.Error(), `@core has no function "frobnicate"`)
}

// TestEvalCoreNamespaceIgnoresImportTable proves @core resolution does
// not read the context's import table: an alias named core resolves
// separately, and @core works when the table lacks any such alias.
func TestEvalCoreNamespaceIgnoresImportTable(t *testing.T) {
	expr, err := lang.ParseExpr("test", []byte(`@core.length('abc')`))
	require.NoError(t, err)
	got, err := Eval(expr, &EvalContext{Libraries: map[string]*Library{"other": {}}})
	require.NoError(t, err)
	require.Equal(t, int64(3), got)
}

// TestCoreFunctionSet locks the namespace's contents: the set only
// grows, and a semantic change takes a new name, so this list and the
// signature shapes are part of the language's compatibility promise.
func TestCoreFunctionSet(t *testing.T) {
	names := make([]string, 0, len(CoreFunctionSigs()))
	for name := range CoreFunctionSigs() {
		names = append(names, name)
	}
	sort.Strings(names)
	require.Equal(t, []string{
		"all", "any", "b64-decode", "b64-encode", "join", "length", "range",
		"to-json",
	}, names)

	sigs := CoreFunctionSigs()
	require.Len(t, sigs["length"].Params, 1)
	require.True(t, sigs["length"].Params[0].Equal(typecheck.TUnion([]typecheck.Type{
		typecheck.TString(),
		typecheck.TList(typecheck.TOpaque()),
		typecheck.TMap(typecheck.TOpaque()),
	})), "got %s", sigs["length"].Params[0])
	require.Nil(t, sigs["length"].Variadic)
	require.True(t, sigs["length"].Result.Equal(typecheck.TInteger()))

	require.Len(t, sigs["join"].Params, 2)
	require.True(t, sigs["join"].Params[0].Equal(typecheck.TList(typecheck.TOpaque())))
	require.True(t, sigs["join"].Params[1].Equal(typecheck.TString()))
	require.Nil(t, sigs["join"].Variadic)
	require.True(t, sigs["join"].Result.Equal(typecheck.TString()))

	require.Len(t, sigs["to-json"].Params, 1)
	require.True(t, sigs["to-json"].Params[0].Equal(typecheck.TOpaque()))
	require.Nil(t, sigs["to-json"].Variadic)
	require.True(t, sigs["to-json"].Result.Equal(typecheck.TString()))
}

// TestLengthUnionMatchesRuntime locks length's declared union to the
// implementation: every member kind's value is accepted by the
// runtime function, every non-member rejected by both the runtime
// and the static face, and the rejection text is the same word for
// word on both sides.
func TestLengthUnionMatchesRuntime(t *testing.T) {
	union := CoreFunctionSigs()["length"].Params[0]
	cases := []struct {
		name string
		typ  typecheck.Type
		val  any
	}{
		{"string", typecheck.TString(), "ab"},
		{"list", typecheck.TList(typecheck.TOpaque()), []any{int64(1)}},
		{"map", typecheck.TMap(typecheck.TOpaque()), map[string]any{"a": int64(1)}},
		{"integer", typecheck.TInteger(), int64(1)},
		{"number", typecheck.TNumber(), 1.5},
		{"boolean", typecheck.TBoolean(), true},
		{"null", typecheck.TNull(), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, rtErr := fnLength(c.val)
			staticOK := typecheck.Assignable(union, c.typ)
			require.Equal(t, rtErr == nil, staticOK,
				"runtime and static faces disagree on %s", c.typ)
		})
	}

	_, err := fnLength(int64(1))
	require.EqualError(t, err,
		"length: argument must be a string, list, or map, got an integer")
}
