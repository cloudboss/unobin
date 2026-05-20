package typecheck

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTypeString(t *testing.T) {
	tests := []struct {
		name string
		typ  Type
		want string
	}{
		{"string", TString(), "string"},
		{"integer", TInteger(), "integer"},
		{"any", TAny(), "any"},
		{"list of string", TList(TString()), "list(string)"},
		{"set of integer", TSet(TInteger()), "set(integer)"},
		{"map of string", TMap(TString()), "map(string)"},
		{"optional string", TOptional(TString()), "optional(string)"},
		{
			"tuple",
			TTuple([]Type{TString(), TInteger(), TBoolean()}),
			"tuple([string integer boolean])",
		},
		{
			"object",
			TObject([]ObjectField{
				{Name: "a", Type: TString()},
				{Name: "b", Type: TInteger()},
			}),
			"object({ a: string  b: integer })",
		},
		{"unknown", TUnknown(), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.typ.String())
		})
	}
}

func TestOptionalCollapsesDouble(t *testing.T) {
	t1 := TOptional(TString())
	t2 := TOptional(t1)
	assert.Equal(t, "optional(string)", t2.String())
}

func TestUnwrap(t *testing.T) {
	assert.True(t, TOptional(TString()).Unwrap().Equal(TString()))
	assert.True(t, TString().Unwrap().Equal(TString()))
}

func TestIsKnown(t *testing.T) {
	assert.False(t, TUnknown().IsKnown())
	assert.False(t, TOptional(TUnknown()).IsKnown())
	assert.True(t, TString().IsKnown())
	assert.True(t, TOptional(TString()).IsKnown())
}

func TestEqual(t *testing.T) {
	assert.True(t, TString().Equal(TString()))
	assert.False(t, TString().Equal(TInteger()))
	assert.True(t, TList(TString()).Equal(TList(TString())))
	assert.False(t, TList(TString()).Equal(TList(TInteger())))

	a := TObject([]ObjectField{
		{Name: "a", Type: TString()},
		{Name: "b", Type: TInteger()},
	})
	b := TObject([]ObjectField{
		{Name: "b", Type: TInteger()},
		{Name: "a", Type: TString()},
	})
	assert.True(t, a.Equal(b), "object field order should not matter")
}

func TestField(t *testing.T) {
	o := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "count", Type: TInteger(), Optional: true},
	})
	id, ok := o.Field("id")
	assert.True(t, ok)
	assert.Equal(t, TString(), id.Type)

	count, ok := o.Field("count")
	assert.True(t, ok)
	assert.True(t, count.Optional)

	_, ok = o.Field("missing")
	assert.False(t, ok)

	_, ok = TString().Field("anything")
	assert.False(t, ok)
}
