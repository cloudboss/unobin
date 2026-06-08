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

func TestInferConditionalJoinsListBranches(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "if true then ['a', 'b'] else []"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TList(TString())), "got %s", got)
	assert.Empty(t, errs.Errors())
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
	got := Infer(parseExpr(t, "{ for s in var.subnets : s.cidr => s.public }"),
		TUnknown(), subnetScope(), errs)
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
	Infer(parseExpr(t, "[ for s in var.subnets : s.cidr when s.cidr ]"),
		TUnknown(), subnetScope(), errs)
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

func TestInferComprehensionRejectsScalarSources(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "count", Type: TInteger()},
			{Name: "name", Type: TString()},
			{Name: "on", Type: TBoolean(), Optional: true},
		},
	}
	tests := []struct {
		src  string
		want string
	}{
		{"[ for x in var.count : x ]",
			"comprehension source must be a list or map, got integer"},
		{"[ for x in var.name : x ]",
			"comprehension source must be a list or map, got string"},
		{"{ for k, v in var.on : k => v }",
			"comprehension source must be a list or map, got optional(boolean)"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Infer(parseExpr(t, tt.src), TUnknown(), scope, errs)
			require.Equal(t, []string{tt.want}, errs.Messages())
		})
	}
}

func TestInferComprehensionRejectsOpaqueSource(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "blob", Type: TOpaque()},
			{Name: "maybe", Type: TOptional(TOpaque())},
			{Name: "items", Type: TList(TOpaque())},
		},
	}
	want := "comprehension source is opaque; declare its type, like list(...) or map(...)"

	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "[ for x in var.blob : x ]"), TUnknown(), scope, errs)
	require.Equal(t, []string{want}, errs.Messages())

	errs = lang.NewErrorList(0)
	Infer(parseExpr(t, "{ for k, v in var.maybe : k => v }"), TUnknown(), scope, errs)
	require.Equal(t, []string{want}, errs.Messages())

	// Elements may be opaque when the container is declared: each
	// binding holds a whole value.
	errs = lang.NewErrorList(0)
	got := Infer(parseExpr(t, "[ for x in var.items : x ]"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TList(TOpaque())), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferComprehensionTupleSource(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "pair", Type: TTuple([]Type{TInteger(), TNumber()})},
		},
	}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "[ for x in var.pair : x ]"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TList(TNumber())), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferComprehensionObjectSource(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "cfg", Type: TObject([]ObjectField{
				{Name: "a", Type: TString()},
				{Name: "b", Type: TString()},
			})},
		},
	}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "{ for k, v in var.cfg : k => v }"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TMap(TString())), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferComprehensionElementAgainstTarget(t *testing.T) {
	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "[ for s in var.subnets : s.cidr ]"), TList(TInteger()), subnetScope(), errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "expected integer, got string")
}
