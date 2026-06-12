package typecheck

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func callScope() *Scope {
	strT := TString()
	anyT := TOpaque()
	lengthUnion := TUnion([]Type{TString(), TList(TOpaque()), TMap(TOpaque())})
	sigs := map[string]FuncSig{
		"b64-encode": {Params: []Type{TString()}, Result: TString()},
		"length":     {Params: []Type{lengthUnion}, Result: TInteger()},
		"all":        {Params: []Type{TList(TBoolean())}, Result: TBoolean()},
		"format":     {Params: []Type{TString()}, Variadic: &anyT, Result: TString()},
		"join":       {Params: []Type{TString()}, Variadic: &strT, Result: TString()},
		"opaque":     {Params: []Type{TUnknown()}, Result: TUnknown()},
	}
	return &Scope{
		LookupFunction: func(library, name string) (FuncSig, bool) {
			if library != "core" {
				return FuncSig{}, false
			}
			sig, ok := sigs[name]
			return sig, ok
		},
	}
}

// hookScope describes one function whose signature computes its
// result from the argument types, recording what the hook receives.
func hookScope(param Type, hook func(args []Type) Type) (*Scope, *[][]Type) {
	var seen [][]Type
	sig := FuncSig{
		Variadic: &param,
		Result:   TUnknown(),
		Infer: func(args []Type) Type {
			seen = append(seen, args)
			return hook(args)
		},
	}
	return &Scope{
		LookupFunction: func(library, name string) (FuncSig, bool) {
			if library != "core" || name != "hooked" {
				return FuncSig{}, false
			}
			return sig, true
		},
	}, &seen
}

func TestInferCallHookComputesResult(t *testing.T) {
	scope, _ := hookScope(TOptional(TObject(nil)), func([]Type) Type { return TString() })
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "core.hooked({ a: 1 })"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TString()), "got %s", got)
	assert.Empty(t, errs.Errors())
}

// TestInferCallHookSeesNaturalArgTypes proves hook arguments are
// inferred without the parameter as a target: an object literal
// reaches the hook as the precise object it spells, not as the
// parameter's own type.
func TestInferCallHookSeesNaturalArgTypes(t *testing.T) {
	scope, seen := hookScope(TOptional(TObject(nil)), func([]Type) Type { return TUnknown() })
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "core.hooked({ a: 1, b: 'x' }, null)"), TUnknown(), scope, errs)
	require.Empty(t, errs.Errors())
	require.Len(t, *seen, 1)
	args := (*seen)[0]
	require.Len(t, args, 2)
	want := TObject([]ObjectField{
		{Name: "a", Type: TInteger()},
		{Name: "b", Type: TString()},
	})
	assert.True(t, args[0].Equal(want), "got %s", args[0])
	assert.True(t, args[1].Equal(TNull()), "got %s", args[1])
}

func TestInferCallHookChecksArguments(t *testing.T) {
	scope, _ := hookScope(TOptional(TObject(nil)), func([]Type) Type { return TUnknown() })
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "core.hooked('nope')"), TUnknown(), scope, errs)
	require.Equal(t,
		[]string{"type mismatch: expected optional(object({  })), got string"},
		errs.Messages())
}

func TestInferCallResultType(t *testing.T) {
	scope := callScope()
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "core.length('abc')"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TInteger()), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferCallUnionParameter(t *testing.T) {
	scope := callScope()

	for _, src := range []string{
		"core.length('abc')",
		"core.length([1, 2])",
		"core.length({ a: 1 })",
	} {
		errs := lang.NewErrorList(0)
		got := Infer(parseExpr(t, src), TUnknown(), scope, errs)
		assert.True(t, got.Equal(TInteger()), "%s -> %s", src, got)
		assert.Empty(t, errs.Errors(), "%s should not error: %v", src, errs.Errors())
	}

	// The rejection reuses the runtime function's own wording, so a
	// compile failure and a plan failure read the same.
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "core.length(5)"), TUnknown(), scope, errs)
	require.Equal(t,
		[]string{"length: argument must be a string, list, or map, got an integer"},
		errs.Messages())

	errs = lang.NewErrorList(0)
	Infer(parseExpr(t, "core.length(true)"), TUnknown(), scope, errs)
	require.Equal(t,
		[]string{"length: argument must be a string, list, or map, got a boolean"},
		errs.Messages())
}

func TestInferCallChecksArguments(t *testing.T) {
	scope := callScope()

	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "core.b64-encode(5)"), TUnknown(), scope, errs)
	assert.Equal(t, 1, errs.Len(), "got: %v", errs.Err())

	errs = lang.NewErrorList(0)
	Infer(parseExpr(t, "core.all([true, false])"), TUnknown(), scope, errs)
	assert.Empty(t, errs.Errors())

	errs = lang.NewErrorList(0)
	Infer(parseExpr(t, "core.all(['a'])"), TUnknown(), scope, errs)
	assert.Equal(t, 1, errs.Len(), "got: %v", errs.Err())
}

func TestInferCallVariadicTail(t *testing.T) {
	scope := callScope()

	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "core.format('%s-%d', 'a', 2)"), TUnknown(), scope, errs)
	assert.Empty(t, errs.Errors(), "an any tail accepts every type: %v", errs.Err())

	errs = lang.NewErrorList(0)
	Infer(parseExpr(t, "core.join('-', 'a', 'b')"), TUnknown(), scope, errs)
	assert.Empty(t, errs.Errors())

	errs = lang.NewErrorList(0)
	Infer(parseExpr(t, "core.join('-', 'a', 5)"), TUnknown(), scope, errs)
	assert.Equal(t, 1, errs.Len(), "got: %v", errs.Err())
}

func TestInferCallResultFeedsTarget(t *testing.T) {
	scope := callScope()

	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "core.length('abc')"), TString(), scope, errs)
	assert.Equal(t, 1, errs.Len(), "an integer result against a string target: %v", errs.Err())

	errs = lang.NewErrorList(0)
	Check(parseExpr(t, "core.b64-encode(core.length('x'))"), TUnknown(), scope, errs)
	assert.Equal(t, 1, errs.Len(),
		"a nested call's result type checks as an argument: %v", errs.Err())
}

func TestInferCallUnknownStaysQuiet(t *testing.T) {
	scope := callScope()

	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "core.nope(1)"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TUnknown()), "existence is the reference checker's job")
	assert.Empty(t, errs.Errors())

	errs = lang.NewErrorList(0)
	got = Infer(parseExpr(t, "other.fn(1)"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TUnknown()))
	assert.Empty(t, errs.Errors())

	errs = lang.NewErrorList(0)
	got = Infer(parseExpr(t, "core.length('a')"), TUnknown(), &Scope{}, errs)
	assert.True(t, got.Equal(TUnknown()), "no lookup hook infers nothing")
	assert.Empty(t, errs.Errors())

	errs = lang.NewErrorList(0)
	Infer(parseExpr(t, "core.b64-encode('a', 'b')"), TUnknown(), scope, errs)
	assert.Empty(t, errs.Errors(), "the argument count is the reference checker's job")

	errs = lang.NewErrorList(0)
	Infer(parseExpr(t, "core.opaque({ a: 1 })"), TUnknown(), scope, errs)
	assert.Empty(t, errs.Errors(), "an unknown parameter type checks nothing")
}
