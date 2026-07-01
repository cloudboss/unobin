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
		{"opaque<-anything", TOpaque(), TList(TString()), true},
		{"string<-opaque", TString(), TOpaque(), false},
		{"string<-unknown", TString(), TUnknown(), true},
		{"unknown<-string", TUnknown(), TString(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Assignable(tt.dst, tt.src))
		})
	}
}

func TestAssignableOpaque(t *testing.T) {
	// In-flow stays free: anything may become opaque.
	assert.True(t, Assignable(TOpaque(), TString()))
	assert.True(t, Assignable(TOpaque(), TNull()))
	assert.True(t, Assignable(TOpaque(), TOptional(TString())))
	assert.True(t, Assignable(TOpaque(), TObject([]ObjectField{{Name: "a", Type: TString()}})))
	assert.True(t, Assignable(TOpaque(), TOpaque()))
	assert.True(t, Assignable(TOptional(TOpaque()), TOpaque()))
	assert.True(t, Assignable(TList(TOpaque()), TList(TString())))
	assert.True(t, Assignable(TMap(TOpaque()), TMap(TInteger())))

	// Out-flow closes: an opaque value flows only into an opaque slot.
	assert.False(t, Assignable(TString(), TOpaque()))
	assert.False(t, Assignable(TBoolean(), TOpaque()))
	assert.False(t, Assignable(TMap(TString()), TOpaque()))
	assert.False(t, Assignable(TList(TString()), TList(TOpaque())))
	assert.False(t, Assignable(TOptional(TString()), TOpaque()))
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

func TestAssignableOpennessIsIrrelevant(t *testing.T) {
	closed := TObject([]ObjectField{{Name: "a", Type: TString()}})
	open := TOpenObject([]ObjectField{{Name: "a", Type: TString()}})
	wider := TObject([]ObjectField{
		{Name: "a", Type: TString()},
		{Name: "b", Type: TInteger()},
	})
	assert.True(t, Assignable(open, closed))
	assert.True(t, Assignable(closed, open))
	assert.True(t, Assignable(open, wider))
	assert.True(t, Assignable(closed, wider))
}

func TestAssignableUnion(t *testing.T) {
	union := TUnion([]Type{TString(), TList(TOpaque()), TMap(TOpaque())})

	assert.True(t, Assignable(union, TString()))
	assert.True(t, Assignable(union, TList(TString())))
	assert.True(t, Assignable(union, TList(TOpaque())))
	assert.True(t, Assignable(union, TMap(TInteger())))
	assert.True(t, Assignable(union, TTuple([]Type{TString(), TInteger()})),
		"a tuple is a list at runtime")
	assert.True(t, Assignable(union, TUnknown()))

	assert.False(t, Assignable(union, TInteger()))
	assert.False(t, Assignable(union, TBoolean()))
	assert.False(t, Assignable(union, TNull()))
	assert.False(t, Assignable(union, TOpaque()),
		"an opaque value flows only into opaque slots")
	assert.False(t, Assignable(union, TOptional(TString())),
		"a possibly-null value wants a null test first")
}

func TestAssignableObjectFromMap(t *testing.T) {
	dst := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "count", Type: TString()},
	})
	assert.True(t, Assignable(dst, TMap(TString())))
	assert.False(t, Assignable(dst, TMap(TInteger())))
}

func TestAssignableLibraryConfig(t *testing.T) {
	fields := []ObjectField{{Name: "region", Type: TString()}}
	aws := TLibraryConfig("example.com/aws", "example.com/aws.Configuration", "abc", fields)
	awsAgain := TLibraryConfig("example.com/aws", "example.com/aws.Configuration", "abc", fields)
	other := TLibraryConfig("example.com/aws", "example.com/aws.Configuration", "def", fields)
	samePathMissingIdentity := TLibraryConfig("example.com/aws", "", "abc", fields)
	sameIdentity := TLibraryConfig(
		"example.com/aws-config",
		"example.com/aws.Configuration",
		"abc",
		fields,
	)
	differentIdentity := TLibraryConfig(
		"example.com/other",
		"example.com/other.Configuration",
		"abc",
		fields,
	)

	assert.True(t, Assignable(aws, awsAgain))
	assert.True(t, Assignable(aws, samePathMissingIdentity))
	assert.True(t, Assignable(aws, sameIdentity))
	assert.False(t, Assignable(aws, other))
	assert.False(t, Assignable(aws, differentIdentity))
	assert.True(t, Assignable(aws, TObject(fields)))
	assert.True(t, Assignable(TObject(fields), aws))
	assert.False(t, Assignable(aws, TObject([]ObjectField{{Name: "region", Type: TInteger()}})))
}
