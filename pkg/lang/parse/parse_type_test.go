package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTypeConstructors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want any
	}{
		{name: "atomic", src: "string", want: &TypeAtomic{Name: "string"}},
		{name: "list", src: "list(string)", want: &TypeList{}},
		{name: "map", src: "map(integer)", want: &TypeMap{}},
		{name: "tuple", src: "tuple(string, integer)", want: &TypeTuple{}},
		{name: "optional", src: "optional(string)", want: &TypeOptional{}},
		{name: "object", src: "object({ host: string port: integer })", want: &TypeObject{}},
		{name: "open", src: "open(object({ tags: map(string) }))", want: &TypeObject{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseType("type.ub", []byte(tt.src))
			require.NoError(t, err)
			require.IsType(t, tt.want, got)
		})
	}
}

func TestParseTypeTupleElements(t *testing.T) {
	got, err := ParseType("type.ub", []byte("tuple(string, integer)"))
	require.NoError(t, err)
	tuple := got.(*TypeTuple)
	require.Len(t, tuple.Elements, 2)
	assert.Equal(t, "string", tuple.Elements[0].(*TypeAtomic).Name)
	assert.Equal(t, "integer", tuple.Elements[1].(*TypeAtomic).Name)
}

func TestParseTypeObjectFields(t *testing.T) {
	got, err := ParseType("type.ub", []byte(`object({
  host: string
  port: { type: integer, default: 8080 }
})`))
	require.NoError(t, err)
	obj := got.(*TypeObject)
	require.Len(t, obj.Fields, 2)
	assert.Equal(t, "host", obj.Fields[0].Name)
	assert.IsType(t, &TypeAtomic{}, obj.Fields[0].Type)
	assert.Equal(t, "port", obj.Fields[1].Name)
	require.NotNil(t, obj.Fields[1].Decl)
	typeField := obj.Fields[1].Decl.Fields[0]
	require.Equal(t, "type", typeField.Key.Name)
	require.IsType(t, &TypeAtomic{}, typeField.Value)
	assert.Equal(t, "integer", typeField.Value.(*TypeAtomic).Name)
}

func TestParseTypeOpenMarksObject(t *testing.T) {
	got, err := ParseType("type.ub", []byte("open(object({ name: string }))"))
	require.NoError(t, err)
	assert.True(t, got.(*TypeObject).Open)
}

func TestParseTypeAtRebasesSpans(t *testing.T) {
	got, err := ParseTypeAt("factory.ub", []byte("open(object({ name: string }))"), Position{
		File:   "factory.ub",
		Line:   7,
		Column: 15,
		Offset: 120,
	})
	require.NoError(t, err)
	assert.Equal(t, Position{File: "factory.ub", Line: 7, Column: 15, Offset: 120},
		got.Span().Start)

	obj := got.(*TypeObject)
	fieldType := obj.Fields[0].Type
	assert.Equal(t, Position{File: "factory.ub", Line: 7, Column: 35, Offset: 140},
		fieldType.Span().Start)
}

func TestParseTypeErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "unknown atomic", src: "frobnicate", want: "unknown atomic type"},
		{name: "unknown nested atomic", src: "list(unknown)", want: "unknown atomic type"},
		{name: "any", src: "any", want: "any is not a type"},
		{name: "optional args", src: "optional(string, integer)", want: "optional takes exactly 1"},
		{name: "tuple args", src: "tuple(string)", want: "tuple takes at least 2"},
		{
			name: "open optional",
			src:  "open(optional(object({ name: string })))",
			want: "open applies to object types",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseType("type.ub", []byte(tt.src))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}
