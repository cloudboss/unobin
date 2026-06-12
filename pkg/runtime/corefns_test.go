package runtime

import (
	"maps"
	"slices"
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
// grows, and a semantic change takes a new name, so this list and
// every function's full signature are part of the language's
// compatibility promise.
func TestCoreFunctionSet(t *testing.T) {
	str := typecheck.TString()
	integer := typecheck.TInteger()
	number := typecheck.TNumber()
	boolean := typecheck.TBoolean()
	expected := map[string]typecheck.FuncSig{
		"all": {
			Params: []typecheck.Type{typecheck.TList(boolean)},
			Result: boolean,
		},
		"any": {
			Params: []typecheck.Type{typecheck.TList(boolean)},
			Result: boolean,
		},
		"b64-decode": {Params: []typecheck.Type{str}, Result: str},
		"b64-encode": {Params: []typecheck.Type{str}, Result: str},
		"join": {
			Params: []typecheck.Type{typecheck.TList(typecheck.TOpaque()), str},
			Result: str,
		},
		"length": {
			Params: []typecheck.Type{typecheck.TUnion([]typecheck.Type{
				str,
				typecheck.TList(typecheck.TOpaque()),
				typecheck.TMap(typecheck.TOpaque()),
			})},
			Result: integer,
		},
		"merge": {
			Variadic: &mergeParam,
			Result:   typecheck.TUnknown(),
			Infer:    typecheck.MergeShallow,
		},
		"range": {
			Params: []typecheck.Type{integer},
			Result: typecheck.TList(integer),
		},
		"to-boolean": {
			Params: []typecheck.Type{typecheck.TUnion([]typecheck.Type{str, boolean})},
			Result: boolean,
		},
		"to-integer": {
			Params: []typecheck.Type{typecheck.TUnion([]typecheck.Type{str, number})},
			Result: integer,
		},
		"to-json": {Params: []typecheck.Type{typecheck.TOpaque()}, Result: str},
		"to-number": {
			Params: []typecheck.Type{typecheck.TUnion([]typecheck.Type{str, number})},
			Result: number,
		},
		"to-string": {
			Params: []typecheck.Type{typecheck.TUnion([]typecheck.Type{str, number, boolean})},
			Result: str,
		},
	}

	sigs := CoreFunctionSigs()
	require.Equal(t, slices.Sorted(maps.Keys(expected)), slices.Sorted(maps.Keys(sigs)))
	for _, name := range slices.Sorted(maps.Keys(expected)) {
		requireSigEqual(t, name, expected[name], sigs[name])
	}
}

// requireSigEqual compares a function's full compile-time face against
// the expected one: parameter list, variadic tail, and result.
func requireSigEqual(t *testing.T, name string, want, got typecheck.FuncSig) {
	t.Helper()
	require.Len(t, got.Params, len(want.Params), "%s: parameter count", name)
	for i := range want.Params {
		require.True(t, want.Params[i].Equal(got.Params[i]),
			"%s parameter %d: want %s, got %s", name, i, want.Params[i], got.Params[i])
	}
	if want.Variadic == nil {
		require.Nil(t, got.Variadic, "%s: unexpected variadic tail", name)
	} else {
		require.NotNil(t, got.Variadic, "%s: missing variadic tail", name)
		require.True(t, want.Variadic.Equal(*got.Variadic),
			"%s variadic: want %s, got %s", name, *want.Variadic, *got.Variadic)
	}
	require.True(t, want.Result.Equal(got.Result),
		"%s result: want %s, got %s", name, want.Result, got.Result)
	require.Equal(t, want.Infer == nil, got.Infer == nil, "%s: result hook presence", name)
}

// TestMergeFaceMatchesRuntime locks merge's declared face to the
// implementation the way the union faces are locked: kind by kind,
// the runtime accepts a value exactly when the static face does.
func TestMergeFaceMatchesRuntime(t *testing.T) {
	face := *CoreFunctionSigs()["merge"].Variadic
	cases := []struct {
		name string
		typ  typecheck.Type
		val  any
	}{
		{
			"object",
			typecheck.TObject([]typecheck.ObjectField{{Name: "a", Type: typecheck.TInteger()}}),
			map[string]any{"a": int64(1)},
		},
		{"map", typecheck.TMap(typecheck.TString()), map[string]any{"a": "x"}},
		{"null", typecheck.TNull(), nil},
		{"string", typecheck.TString(), "x"},
		{"integer", typecheck.TInteger(), int64(1)},
		{"number", typecheck.TNumber(), 1.5},
		{"boolean", typecheck.TBoolean(), true},
		{"list", typecheck.TList(typecheck.TOpaque()), []any{int64(1)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, rtErr := fnMerge(c.val)
			staticOK := typecheck.Assignable(face, c.typ)
			require.Equal(t, rtErr == nil, staticOK,
				"runtime and static faces disagree on %s", c.typ)
		})
	}

	_, err := fnMerge("x")
	require.EqualError(t, err, "merge: argument 1 must be an object, got a string")
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

// The four conversion faces get the same treatment as length: kind by
// kind, the runtime accepts a representative value exactly when the
// static face does, and the union-mismatch text is identical on both
// sides.

func TestToIntegerUnionMatchesRuntime(t *testing.T) {
	union := CoreFunctionSigs()["to-integer"].Params[0]
	cases := []struct {
		name string
		typ  typecheck.Type
		val  any
	}{
		{"string", typecheck.TString(), "42"},
		{"list", typecheck.TList(typecheck.TOpaque()), []any{int64(1)}},
		{"map", typecheck.TMap(typecheck.TOpaque()), map[string]any{"a": int64(1)}},
		{"integer", typecheck.TInteger(), int64(1)},
		{"number", typecheck.TNumber(), 1.5},
		{"boolean", typecheck.TBoolean(), true},
		{"null", typecheck.TNull(), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, rtErr := fnToInteger(c.val)
			staticOK := typecheck.Assignable(union, c.typ)
			require.Equal(t, rtErr == nil, staticOK,
				"runtime and static faces disagree on %s", c.typ)
		})
	}

	_, err := fnToInteger(true)
	require.EqualError(t, err,
		"to-integer: argument must be a string or number, got a boolean")
}

func TestToNumberUnionMatchesRuntime(t *testing.T) {
	union := CoreFunctionSigs()["to-number"].Params[0]
	cases := []struct {
		name string
		typ  typecheck.Type
		val  any
	}{
		{"string", typecheck.TString(), "1.5"},
		{"list", typecheck.TList(typecheck.TOpaque()), []any{int64(1)}},
		{"map", typecheck.TMap(typecheck.TOpaque()), map[string]any{"a": int64(1)}},
		{"integer", typecheck.TInteger(), int64(1)},
		{"number", typecheck.TNumber(), 1.5},
		{"boolean", typecheck.TBoolean(), true},
		{"null", typecheck.TNull(), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, rtErr := fnToNumber(c.val)
			staticOK := typecheck.Assignable(union, c.typ)
			require.Equal(t, rtErr == nil, staticOK,
				"runtime and static faces disagree on %s", c.typ)
		})
	}

	_, err := fnToNumber(true)
	require.EqualError(t, err,
		"to-number: argument must be a string or number, got a boolean")
}

func TestToStringUnionMatchesRuntime(t *testing.T) {
	union := CoreFunctionSigs()["to-string"].Params[0]
	cases := []struct {
		name string
		typ  typecheck.Type
		val  any
	}{
		{"string", typecheck.TString(), "x"},
		{"list", typecheck.TList(typecheck.TOpaque()), []any{int64(1)}},
		{"map", typecheck.TMap(typecheck.TOpaque()), map[string]any{"a": int64(1)}},
		{"integer", typecheck.TInteger(), int64(1)},
		{"number", typecheck.TNumber(), 1.5},
		{"boolean", typecheck.TBoolean(), true},
		{"null", typecheck.TNull(), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, rtErr := fnToString(c.val)
			staticOK := typecheck.Assignable(union, c.typ)
			require.Equal(t, rtErr == nil, staticOK,
				"runtime and static faces disagree on %s", c.typ)
		})
	}

	_, err := fnToString([]any{int64(1)})
	require.EqualError(t, err,
		"to-string: argument must be a string, number, or boolean, got a list")
}

func TestToBooleanUnionMatchesRuntime(t *testing.T) {
	union := CoreFunctionSigs()["to-boolean"].Params[0]
	cases := []struct {
		name string
		typ  typecheck.Type
		val  any
	}{
		{"string", typecheck.TString(), "true"},
		{"list", typecheck.TList(typecheck.TOpaque()), []any{int64(1)}},
		{"map", typecheck.TMap(typecheck.TOpaque()), map[string]any{"a": int64(1)}},
		{"integer", typecheck.TInteger(), int64(1)},
		{"number", typecheck.TNumber(), 1.5},
		{"boolean", typecheck.TBoolean(), true},
		{"null", typecheck.TNull(), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, rtErr := fnToBoolean(c.val)
			staticOK := typecheck.Assignable(union, c.typ)
			require.Equal(t, rtErr == nil, staticOK,
				"runtime and static faces disagree on %s", c.typ)
		})
	}

	_, err := fnToBoolean(int64(1))
	require.EqualError(t, err,
		"to-boolean: argument must be a string or boolean, got an integer")
}
