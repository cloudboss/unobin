package typecheck

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
)

func callScope() *Scope {
	strT := TString()
	anyT := TAny()
	sigs := map[string]FuncSig{
		"b64-encode": {Params: []Type{TString()}, Result: TString()},
		"length":     {Params: []Type{TAny()}, Result: TInteger()},
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

func TestInferCallResultType(t *testing.T) {
	scope := callScope()
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "core.length('abc')"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TInteger()), "got %s", got)
	assert.Empty(t, errs.Errors())
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
