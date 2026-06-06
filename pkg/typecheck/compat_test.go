package typecheck

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAssignableAtomics(t *testing.T) {
	tests := []struct {
		name string
		dst  Type
		src  Type
		want bool
	}{
		{"string<-string", TString(), TString(), true},
		{"string<-integer", TString(), TInteger(), false},
		{"integer<-integer", TInteger(), TInteger(), true},
		{"integer<-number", TInteger(), TNumber(), false},
		{"number<-integer", TNumber(), TInteger(), true},
		{"number<-number", TNumber(), TNumber(), true},
		{"boolean<-boolean", TBoolean(), TBoolean(), true},
		{"boolean<-string", TBoolean(), TString(), false},
		{"any<-anything", TOpaque(), TList(TString()), true},
		{"string<-any", TString(), TOpaque(), true},
		{"string<-unknown", TString(), TUnknown(), true},
		{"unknown<-string", TUnknown(), TString(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Assignable(tt.dst, tt.src))
		})
	}
}

func TestAssignableOptionalAndNull(t *testing.T) {
	assert.True(t, Assignable(TOptional(TString()), TString()))
	assert.True(t, Assignable(TOptional(TString()), TNull()))
	assert.False(t, Assignable(TString(), TOptional(TString())),
		"a possibly-null value does not flow into a slot that wants a value")
	assert.True(t, Assignable(TOptional(TString()), TOptional(TString())))
	assert.False(t, Assignable(TString(), TNull()))
	assert.False(t, Assignable(TOptional(TInteger()), TString()))
}

func TestAssignableLists(t *testing.T) {
	assert.True(t, Assignable(TList(TString()), TList(TString())))
	assert.False(t, Assignable(TList(TString()), TList(TInteger())))
	assert.True(t, Assignable(TList(TNumber()), TList(TInteger())))
	assert.True(t, Assignable(TList(TString()), TTuple([]Type{TString(), TString()})))
	assert.False(t, Assignable(TList(TString()), TTuple([]Type{TString(), TInteger()})))
}

func TestAssignableMaps(t *testing.T) {
	assert.True(t, Assignable(TMap(TString()), TMap(TString())))
	assert.False(t, Assignable(TMap(TString()), TMap(TInteger())))
	assert.True(t, Assignable(TMap(TString()), TObject([]ObjectField{
		{Name: "a", Type: TString()},
		{Name: "b", Type: TString()},
	})))
	assert.False(t, Assignable(TMap(TString()), TObject([]ObjectField{
		{Name: "a", Type: TString()},
		{Name: "b", Type: TInteger()},
	})))
}

func TestAssignableTuples(t *testing.T) {
	assert.True(
		t,
		Assignable(
			TTuple([]Type{TString(), TInteger()}),
			TTuple([]Type{TString(), TInteger()}),
		),
	)
	assert.False(
		t,
		Assignable(
			TTuple([]Type{TString(), TInteger()}),
			TTuple([]Type{TString(), TString()}),
		),
	)
	assert.False(
		t,
		Assignable(
			TTuple([]Type{TString()}),
			TTuple([]Type{TString(), TString()}),
		),
	)
}

func TestAssignableObjects(t *testing.T) {
	dst := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "tags", Type: TMap(TString()), Optional: true},
	})

	src := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "tags", Type: TMap(TString())},
	})
	assert.True(t, Assignable(dst, src))

	missingOptional := TObject([]ObjectField{
		{Name: "id", Type: TString()},
	})
	assert.True(t, Assignable(dst, missingOptional))

	missingRequired := TObject([]ObjectField{
		{Name: "tags", Type: TMap(TString())},
	})
	assert.False(t, Assignable(dst, missingRequired))

	wrongType := TObject([]ObjectField{
		{Name: "id", Type: TInteger()},
	})
	assert.False(t, Assignable(dst, wrongType))

	extra := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "tags", Type: TMap(TString())},
		{Name: "color", Type: TString()},
	})
	assert.True(t, Assignable(dst, extra),
		"extra src fields are tolerated; dst declares its own fields")
}

func TestAssignableObjectFromMap(t *testing.T) {
	dst := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "count", Type: TString()},
	})
	assert.True(t, Assignable(dst, TMap(TString())))
	assert.False(t, Assignable(dst, TMap(TInteger())))
}
