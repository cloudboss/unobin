package ub

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalAtoms(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "nil", in: nil, want: "null"},
		{name: "true", in: true, want: "true"},
		{name: "false", in: false, want: "false"},
		{name: "int", in: 42, want: "42"},
		{name: "int64", in: int64(-7), want: "-7"},
		{name: "uint", in: uint(9), want: "9"},
		{name: "float64", in: 3.5, want: "3.5"},
		{name: "float32", in: float32(0.25), want: "0.25"},
		{name: "string plain", in: "hello", want: "'hello'"},
		{name: "string apostrophe", in: "It's", want: `'It\'s'`},
		{name: "string backslash", in: `a\b`, want: `'a\\b'`},
		{name: "string newline", in: "a\nb", want: `'a\nb'`},
		{name: "string tab", in: "a\tb", want: `'a\tb'`},
		{name: "empty list", in: []any{}, want: "[]"},
		{name: "empty map", in: map[string]any{}, want: "{}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestMarshalLists(t *testing.T) {
	got, err := Marshal([]any{1, "two", true, nil})
	require.NoError(t, err)
	assert.Equal(t, "[1, 'two', true, null]", string(got))
}

func TestMarshalMaps(t *testing.T) {
	got, err := Marshal(map[string]any{"b": 2, "a": 1})
	require.NoError(t, err)
	assert.Equal(t, "{ a: 1, b: 2 }", string(got))
}

func TestMarshalQuotedKey(t *testing.T) {
	got, err := Marshal(map[string]any{"has space": 1})
	require.NoError(t, err)
	assert.Equal(t, "{ 'has space': 1 }", string(got))
}

func TestMarshalNested(t *testing.T) {
	in := map[string]any{
		"a": []any{1, 2},
		"b": map[string]any{"c": "d"},
	}
	got, err := Marshal(in)
	require.NoError(t, err)
	assert.Equal(t, "{ a: [1, 2], b: { c: 'd' } }", string(got))
}

func TestMarshalPointer(t *testing.T) {
	n := 42
	got, err := Marshal(&n)
	require.NoError(t, err)
	assert.Equal(t, "42", string(got))

	var nilPtr *int
	got, err = Marshal(nilPtr)
	require.NoError(t, err)
	assert.Equal(t, "null", string(got))
}

func TestMarshalBytes(t *testing.T) {
	got, err := Marshal([]byte{0x01, 0x02, 0x03})
	require.NoError(t, err)
	assert.Equal(t, "'AQID'", string(got))
}

func TestMarshalStruct(t *testing.T) {
	type Foo struct {
		BarBaz string
		Count  int
	}
	got, err := Marshal(Foo{BarBaz: "hi", Count: 3})
	require.NoError(t, err)
	assert.Equal(t, "{ bar-baz: 'hi', count: 3 }", string(got))
}

func TestMarshalStructTags(t *testing.T) {
	type Foo struct {
		A string `ub:"alpha"`
		B string `ub:"beta,omitempty"`
		C string `ub:"-"`
		D string `ub:",omitempty"`
		E string
	}
	tests := []struct {
		name string
		in   Foo
		want string
	}{
		{
			name: "all populated",
			in:   Foo{A: "a", B: "b", C: "skip", D: "d", E: "e"},
			want: "{ alpha: 'a', beta: 'b', d: 'd', e: 'e' }",
		},
		{
			name: "omitempty drops zero",
			in:   Foo{A: "a", E: "e"},
			want: "{ alpha: 'a', e: 'e' }",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestMarshalUnexportedFieldsSkipped(t *testing.T) {
	type foo struct {
		Public  string
		private string
	}
	got, err := Marshal(foo{Public: "p", private: "s"})
	require.NoError(t, err)
	assert.Equal(t, "{ public: 'p' }", string(got))
}

func TestMarshalTime(t *testing.T) {
	when := time.Date(2026, 5, 20, 15, 4, 5, 0, time.UTC)
	got, err := Marshal(when)
	require.NoError(t, err)
	assert.Equal(t, "'2026-05-20T15:04:05Z'", string(got))
}

func TestMarshalDuration(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{name: "seconds", in: 30 * time.Second, want: "'30s'"},
		{name: "milliseconds", in: 1500 * time.Millisecond, want: "'1.5s'"},
		{name: "microseconds", in: 711785 * time.Nanosecond, want: "'711.785us'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

type customMarshaler struct {
	tag string
}

func (c customMarshaler) MarshalUB() ([]byte, error) {
	return []byte("'custom:" + c.tag + "'"), nil
}

type erroringMarshaler struct{}

func (erroringMarshaler) MarshalUB() ([]byte, error) {
	return nil, errors.New("boom")
}

func TestMarshalerInterface(t *testing.T) {
	got, err := Marshal(customMarshaler{tag: "x"})
	require.NoError(t, err)
	assert.Equal(t, "'custom:x'", string(got))
}

func TestMarshalerError(t *testing.T) {
	_, err := Marshal(erroringMarshaler{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestMarshalUnsupported(t *testing.T) {
	ch := make(chan int)
	_, err := Marshal(ch)
	require.Error(t, err)
}

func TestMarshalIndentAtom(t *testing.T) {
	got, err := MarshalIndent("hello", "", "  ")
	require.NoError(t, err)
	assert.Equal(t, "'hello'", string(got))
}

func TestMarshalIndentEmpty(t *testing.T) {
	got, err := MarshalIndent([]any{}, "", "  ")
	require.NoError(t, err)
	assert.Equal(t, "[]", string(got))

	got, err = MarshalIndent(map[string]any{}, "", "  ")
	require.NoError(t, err)
	assert.Equal(t, "{}", string(got))
}

func TestMarshalIndentList(t *testing.T) {
	got, err := MarshalIndent([]any{1, 2, 3}, "", "  ")
	require.NoError(t, err)
	want := "[\n  1,\n  2,\n  3,\n]"
	assert.Equal(t, want, string(got))
}

func TestMarshalIndentMap(t *testing.T) {
	got, err := MarshalIndent(map[string]any{"a": 1, "b": 2}, "", "  ")
	require.NoError(t, err)
	want := "{\n  a: 1\n  b: 2\n}"
	assert.Equal(t, want, string(got))
}

func TestMarshalIndentNested(t *testing.T) {
	in := map[string]any{
		"alpha": []any{1, 2},
		"beta":  map[string]any{"gamma": "delta"},
	}
	got, err := MarshalIndent(in, "", "  ")
	require.NoError(t, err)
	want := "{\n  alpha: [\n    1,\n    2,\n  ]\n  beta: {\n    gamma: 'delta'\n  }\n}"
	assert.Equal(t, want, string(got))
}

func TestMarshalIndentPrefix(t *testing.T) {
	got, err := MarshalIndent([]any{1, 2}, ">>", "  ")
	require.NoError(t, err)
	want := "[\n>>  1,\n>>  2,\n>>]"
	assert.Equal(t, want, string(got))
}
