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
		{"opaque", TOpaque(), "opaque"},
		{"list of string", TList(TString()), "list(string)"},
		{"map of string", TMap(TString()), "map(string)"},
		{"optional string", TOptional(TString()), "optional(string)"},
		{
			"tuple",
			TTuple([]Type{TString(), TInteger(), TBoolean()}),
			"tuple(string, integer, boolean)",
		},
		{
			"object",
			TObject([]ObjectField{
				{Name: "a", Type: TString()},
				{Name: "b", Type: TInteger()},
			}),
			"object({ a: string  b: integer })",
		},
		{
			"library config",
			TLibraryConfig("github.com/acme/aws", "github.com/acme/aws", "abc", nil),
			"library-config('github.com/acme/aws')",
		},
		{
			"open object",
			TOpenObject([]ObjectField{{Name: "a", Type: TString()}}),
			"open(object({ a: string }))",
		},
		{
			"union",
			TUnion([]Type{TString(), TList(TOpaque()), TMap(TOpaque())}),
			"string | list(opaque) | map(opaque)",
		},
		{"unknown", TUnknown(), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.typ.String())
		})
	}
}

func TestContainsUnknown(t *testing.T) {
	tests := []struct {
		name string
		typ  Type
		want bool
	}{
		{"unknown itself", TUnknown(), true},
		{"string", TString(), false},
		{"opaque", TOpaque(), false},
		{"list of unknown", TList(TUnknown()), true},
		{"list of string", TList(TString()), false},
		{"optional of unknown", TOptional(TUnknown()), true},
		{"deep map", TMap(TList(TUnknown())), true},
		{"tuple with unknown member", TTuple([]Type{TString(), TUnknown()}), true},
		{"tuple of knowns", TTuple([]Type{TString(), TInteger()}), false},
		{
			"object with unknown field",
			TObject([]ObjectField{{Name: "a", Type: TUnknown()}}),
			true,
		},
		{
			"object of knowns",
			TObject([]ObjectField{{Name: "a", Type: TString()}}),
			false,
		},
		{
			"nested object unknown",
			TObject([]ObjectField{{
				Name: "a",
				Type: TObject([]ObjectField{{Name: "b", Type: TUnknown()}}),
			}}),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.typ.ContainsUnknown())
		})
	}
}

func TestEqualDistinguishesOpenObjects(t *testing.T) {
	closed := TObject([]ObjectField{{Name: "a", Type: TString()}})
	open := TOpenObject([]ObjectField{{Name: "a", Type: TString()}})
	assert.False(t, closed.Equal(open))
	assert.False(t, open.Equal(closed))
	assert.True(t, open.Equal(TOpenObject([]ObjectField{{Name: "a", Type: TString()}})))
}

func TestEqualDistinguishesLibraryConfigIdentity(t *testing.T) {
	fields := []ObjectField{{Name: "region", Type: TString()}}
	aws := TLibraryConfig("github.com/acme/aws", "github.com/acme/aws", "abc", fields)
	awsAgain := TLibraryConfig("github.com/acme/aws", "github.com/acme/aws", "abc", fields)
	other := TLibraryConfig("github.com/acme/aws", "github.com/acme/aws", "def", fields)

	assert.True(t, aws.Equal(awsAgain))
	assert.False(t, aws.Equal(other))
	assert.False(t, aws.Equal(TObject(fields)))
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

	cfg := TLibraryConfig("github.com/acme/aws", "github.com/acme/aws", "abc", []ObjectField{
		{Name: "region", Type: TString()},
	})
	region, ok := cfg.Field("region")
	assert.True(t, ok)
	assert.Equal(t, TString(), region.Type)

	_, ok = TString().Field("anything")
	assert.False(t, ok)
}
