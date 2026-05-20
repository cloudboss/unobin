package typecheck

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseExpr(t *testing.T, src string) lang.Expr {
	t.Helper()
	f, err := lang.ParseSource("expr.ub", []byte("v: "+src))
	require.NoError(t, err)
	require.NotNil(t, f.Body)
	require.NotEmpty(t, f.Body.Fields)
	return f.Body.Fields[0].Value
}

func TestInferLiterals(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)

	tests := []struct {
		src  string
		want Type
	}{
		{"'hi'", TString()},
		{"42", TInteger()},
		{"3.14", TNumber()},
		{"true", TBoolean()},
		{"null", TNull()},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			got := Infer(parseExpr(t, tt.src), TUnknown(), scope, errs)
			assert.True(t, got.Equal(tt.want), "got %s want %s", got, tt.want)
		})
	}
	assert.Empty(t, errs.Errors())
}

func TestInferArrayUntargetedJoins(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "[1, 2, 3]"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TList(TInteger())))
	assert.Empty(t, errs.Errors())
}

func TestInferArrayUntargetedMixedBecomesTuple(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "[1, 'hi', true]"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TTuple([]Type{TInteger(), TString(), TBoolean()})))
	assert.Empty(t, errs.Errors())
}

func TestInferArrayMatchesListTarget(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	target := TList(TString())
	got := Check(parseExpr(t, "['a', 'b']"), target, scope, errs)
	assert.True(t, got.Equal(target))
	assert.Empty(t, errs.Errors())
}

func TestInferArrayElementMismatch(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	target := TList(TString())
	Check(parseExpr(t, "['a', 5]"), target, scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "expected string, got integer")
}

func TestInferArrayTupleTarget(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	target := TTuple([]Type{TString(), TInteger()})
	got := Check(parseExpr(t, "['a', 5]"), target, scope, errs)
	assert.True(t, got.Equal(target))
	assert.Empty(t, errs.Errors())
}

func TestInferObjectAgainstClosedTarget(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	target := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "count", Type: TInteger(), Optional: true},
	})
	got := Check(parseExpr(t, "{ id: 'x' }"), target, scope, errs)
	assert.Equal(t, Object, got.Kind)
	assert.Empty(t, errs.Errors())
}

func TestInferObjectReportsUnknownField(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	target := TObject([]ObjectField{{Name: "id", Type: TString()}})
	Check(parseExpr(t, "{ id: 'x', bogus: 1 }"), target, scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, `unknown field "bogus"`)
}

func TestInferObjectReportsMissingRequired(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	target := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "count", Type: TInteger()},
	})
	Check(parseExpr(t, "{ id: 'x' }"), target, scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, `missing required field "count"`)
}

func TestInferVar(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "region", Type: TString()},
			{Name: "ports", Type: TList(TInteger())},
			{Name: "tags", Type: TMap(TString()), Optional: true},
		},
	}
	errs := lang.NewErrorList(0)

	assert.True(t, Infer(parseExpr(t, "var.region"), TUnknown(), scope, errs).Equal(TString()))
	assert.True(t, Infer(parseExpr(t, "var.ports"), TUnknown(), scope, errs).Equal(TList(TInteger())))
	assert.True(
		t,
		Infer(parseExpr(t, "var.tags"), TUnknown(), scope, errs).
			Equal(TOptional(TMap(TString()))),
	)
	assert.Empty(t, errs.Errors())
}

func TestInferVarIntoStringSlotIntegerMismatch(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "count", Type: TInteger()},
		},
	}
	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "var.count"), TString(), scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "expected string, got integer")
}

func TestInferNodeFieldTraversal(t *testing.T) {
	output := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "tags", Type: TMap(TString())},
	})
	scope := &Scope{
		LookupNode: func(kind, ns, typ, name string) (Type, bool) {
			if kind == "resource" && ns == "aws" && typ == "vpc" && name == "main" {
				return output, true
			}
			return Type{}, false
		},
	}
	errs := lang.NewErrorList(0)

	got := Infer(
		parseExpr(t, "resource.aws.vpc.main.id"),
		TUnknown(), scope, errs,
	)
	assert.True(t, got.Equal(TString()))

	got = Infer(
		parseExpr(t, "resource.aws.vpc.main.tags.name"),
		TUnknown(), scope, errs,
	)
	assert.True(t, got.Equal(TString()))

	got = Infer(
		parseExpr(t, "resource.aws.vpc.main.bogus"),
		TUnknown(), scope, errs,
	)
	assert.True(t, got.Equal(TUnknown()),
		"missing field traversal returns Unknown; existing reference checker reports the message")
	assert.Empty(t, errs.Errors())
}

func TestInferVarNestedObjectReportsUnknownField(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "cfg", Type: TObject([]ObjectField{
				{Name: "host", Type: TString()},
				{Name: "port", Type: TInteger()},
			})},
		},
	}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "var.cfg.bogus"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TUnknown()))
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, `unknown field "bogus" on object(`)
}

func TestInferVarDeeplyNestedObjectReportsUnknownField(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "cfg", Type: TObject([]ObjectField{
				{Name: "db", Type: TObject([]ObjectField{
					{Name: "host", Type: TString()},
				})},
			})},
		},
	}
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "var.cfg.db.port"), TUnknown(), scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, `unknown field "port" on object(`)
}

func TestInferNodeDeepNestedObjectReportsUnknownField(t *testing.T) {
	output := TObject([]ObjectField{
		{Name: "endpoint", Type: TObject([]ObjectField{
			{Name: "host", Type: TString()},
		})},
	})
	scope := &Scope{
		LookupNode: func(kind, ns, typ, name string) (Type, bool) {
			if kind == "resource" && ns == "aws" && typ == "rds" && name == "main" {
				return output, true
			}
			return Type{}, false
		},
	}
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "resource.aws.rds.main.endpoint.port"), TUnknown(), scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, `unknown field "port" on object(`)
}

func TestInferNodeFirstSegmentUnknownStaysSilent(t *testing.T) {
	output := TObject([]ObjectField{
		{Name: "id", Type: TString()},
	})
	scope := &Scope{
		LookupNode: func(kind, ns, typ, name string) (Type, bool) {
			if kind == "resource" && ns == "aws" && typ == "vpc" && name == "main" {
				return output, true
			}
			return Type{}, false
		},
	}
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "resource.aws.vpc.main.bogus"), TUnknown(), scope, errs)
	assert.Empty(t, errs.Errors(),
		"first trailing segment is the reference checker's responsibility")
}

func TestInferEachKeyValue(t *testing.T) {
	scope := &Scope{
		Each: &EachBinding{Key: TString(), Value: TInteger()},
	}
	errs := lang.NewErrorList(0)
	assert.True(
		t,
		Infer(parseExpr(t, "@each.key"), TUnknown(), scope, errs).Equal(TString()),
	)
	assert.True(
		t,
		Infer(parseExpr(t, "@each.value"), TUnknown(), scope, errs).Equal(TInteger()),
	)
	assert.Empty(t, errs.Errors())
}

func TestInferInfixOperators(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	tests := []struct {
		src  string
		want Type
	}{
		{"1 == 2", TBoolean()},
		{"'a' != 'b'", TBoolean()},
		{"true && false", TBoolean()},
		{"1 + 2", TInteger()},
		{"1 + 2.0", TNumber()},
		{"'a' + 'b'", TString()},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			got := Infer(parseExpr(t, tt.src), TUnknown(), scope, errs)
			assert.True(t, got.Equal(tt.want), "got %s want %s", got, tt.want)
		})
	}
	assert.Empty(t, errs.Errors())
}

func TestInferUnknownSkipsCheck(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	got := Check(parseExpr(t, "'hi'"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TString()))
	assert.Empty(t, errs.Errors())
}
