package typecheck

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInferConditional(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)

	got := Infer(parseExpr(t, "if true then 'a' else 'b'"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TString()), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferConditionalJoinsNumeric(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "if true then 1 else 2.0"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TNumber()), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferConditionalNonBoolCondition(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "if 5 then 'a' else 'b'"), TUnknown(), scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "expected boolean, got integer")
}

func TestInferConditionalBranchMismatch(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "if true then 'a' else 1"), TUnknown(), scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "branches have different types")
}

func TestInferConditionalAgainstTarget(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "if true then 1 else 2"), TString(), scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "expected string, got integer")
}

func subnetScope() *Scope {
	return &Scope{
		Inputs: []ObjectField{
			{Name: "subnets", Type: TList(TObject([]ObjectField{
				{Name: "cidr", Type: TString()},
				{Name: "public", Type: TBoolean()},
			}))},
			{Name: "nums", Type: TList(TInteger())},
			{Name: "m", Type: TMap(TString())},
			{Name: "things", Type: TSet(TString())},
		},
	}
}

func TestInferListComprehension(t *testing.T) {
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "[ for s in var.subnets : s.cidr ]"), TUnknown(), subnetScope(), errs)
	assert.True(t, got.Equal(TList(TString())), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferListComprehensionBareBoundValue(t *testing.T) {
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "[ for n in var.nums : n ]"), TUnknown(), subnetScope(), errs)
	assert.True(t, got.Equal(TList(TInteger())), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferComprehensionUnknownBoundField(t *testing.T) {
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "[ for s in var.subnets : s.bogus ]"), TUnknown(), subnetScope(), errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, `unknown field "bogus"`)
}

func TestInferMapComprehension(t *testing.T) {
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "{ for s in var.subnets : s.cidr => s.public }"), TUnknown(), subnetScope(), errs)
	assert.True(t, got.Equal(TMap(TBoolean())), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferMapComprehensionGroupBy(t *testing.T) {
	errs := lang.NewErrorList(0)
	got := Infer(
		parseExpr(t, "{ for s in var.subnets : s.cidr => s.public... }"), TUnknown(), subnetScope(), errs)
	assert.True(t, got.Equal(TMap(TList(TBoolean()))), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferMapComprehensionNonStringKey(t *testing.T) {
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "{ for n in var.nums : n => n }"), TUnknown(), subnetScope(), errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "expected string, got integer")
}

func TestInferComprehensionNonBoolFilter(t *testing.T) {
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "[ for s in var.subnets : s.cidr when s.cidr ]"), TUnknown(), subnetScope(), errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "expected boolean, got string")
}

func TestInferComprehensionListIndexBinding(t *testing.T) {
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "[ for i, s in var.subnets : i ]"), TUnknown(), subnetScope(), errs)
	assert.True(t, got.Equal(TList(TInteger())), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferComprehensionMapKeyValueBinding(t *testing.T) {
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "{ for k, v in var.m : k => v }"), TUnknown(), subnetScope(), errs)
	assert.True(t, got.Equal(TMap(TString())), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferComprehensionRejectsSetSource(t *testing.T) {
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "[ for x in var.things : x ]"), TUnknown(), subnetScope(), errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "cannot be a set")
}

func TestInferComprehensionElementAgainstTarget(t *testing.T) {
	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "[ for s in var.subnets : s.cidr ]"), TList(TInteger()), subnetScope(), errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "expected integer, got string")
}
