package ub

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalAtomsIntoTyped(t *testing.T) {
	var b bool
	require.NoError(t, Unmarshal([]byte("true"), &b))
	assert.True(t, b)

	var n int
	require.NoError(t, Unmarshal([]byte("42"), &n))
	assert.Equal(t, 42, n)

	var i64 int64
	require.NoError(t, Unmarshal([]byte("-7"), &i64))
	assert.Equal(t, int64(-7), i64)

	var f float64
	require.NoError(t, Unmarshal([]byte("3.5"), &f))
	assert.Equal(t, 3.5, f)

	var s string
	require.NoError(t, Unmarshal([]byte("'hello'"), &s))
	assert.Equal(t, "hello", s)
}

func TestUnmarshalNull(t *testing.T) {
	var p *int
	require.NoError(t, Unmarshal([]byte("null"), &p))
	assert.Nil(t, p)

	var s []string
	require.NoError(t, Unmarshal([]byte("null"), &s))
	assert.Nil(t, s)

	var m map[string]int
	require.NoError(t, Unmarshal([]byte("null"), &m))
	assert.Nil(t, m)

	var a any = "before"
	require.NoError(t, Unmarshal([]byte("null"), &a))
	assert.Nil(t, a)
}

func TestUnmarshalIntoAny(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want any
	}{
		{name: "bool", in: "true", want: true},
		{name: "int", in: "42", want: int64(42)},
		{name: "float", in: "3.5", want: 3.5},
		{name: "string", in: "'hello'", want: "hello"},
		{name: "null", in: "null", want: nil},
		{
			name: "list",
			in:   "[1, 'two', true]",
			want: []any{int64(1), "two", true},
		},
		{
			name: "map",
			in:   "{ a: 1, b: 'x' }",
			want: map[string]any{"a": int64(1), "b": "x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got any
			require.NoError(t, Unmarshal([]byte(tt.in), &got))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUnmarshalList(t *testing.T) {
	var s []int
	require.NoError(t, Unmarshal([]byte("[1, 2, 3]"), &s))
	assert.Equal(t, []int{1, 2, 3}, s)

	var strs []string
	require.NoError(t, Unmarshal([]byte("['a', 'b']"), &strs))
	assert.Equal(t, []string{"a", "b"}, strs)

	var empty []int
	require.NoError(t, Unmarshal([]byte("[]"), &empty))
	assert.Equal(t, []int{}, empty)
}

func TestUnmarshalMap(t *testing.T) {
	var m map[string]int
	require.NoError(t, Unmarshal([]byte("{ a: 1, b: 2 }"), &m))
	assert.Equal(t, map[string]int{"a": 1, "b": 2}, m)

	var empty map[string]string
	require.NoError(t, Unmarshal([]byte("{}"), &empty))
	assert.Equal(t, map[string]string{}, empty)
}

func TestUnmarshalStruct(t *testing.T) {
	type Foo struct {
		BarBaz string
		Count  int
	}
	var got Foo
	require.NoError(t, Unmarshal([]byte("{ bar-baz: 'hi', count: 3 }"), &got))
	assert.Equal(t, Foo{BarBaz: "hi", Count: 3}, got)
}

func TestUnmarshalStructTags(t *testing.T) {
	type Foo struct {
		A string `ub:"alpha"`
		B string `ub:"beta,omitempty"`
		C string `ub:"-"`
		D string
	}
	var got Foo
	require.NoError(t, Unmarshal([]byte("{ alpha: 'a', beta: 'b', d: 'd' }"), &got))
	assert.Equal(t, Foo{A: "a", B: "b", D: "d"}, got)
}

func TestUnmarshalStructIgnoresUnknownFields(t *testing.T) {
	type Foo struct {
		A string
	}
	var got Foo
	require.NoError(t, Unmarshal([]byte("{ a: 'x', extra: 1 }"), &got))
	assert.Equal(t, Foo{A: "x"}, got)
}

func TestUnmarshalNested(t *testing.T) {
	type Inner struct {
		Name string
	}
	type Outer struct {
		Tag  string
		List []int
		In   Inner
	}
	src := "{ tag: 'web', list: [1, 2, 3], in: { name: 'inside' } }"
	var got Outer
	require.NoError(t, Unmarshal([]byte(src), &got))
	assert.Equal(t, Outer{Tag: "web", List: []int{1, 2, 3}, In: Inner{Name: "inside"}}, got)
}

func TestUnmarshalPointerField(t *testing.T) {
	type Foo struct {
		N *int
	}
	var got Foo
	require.NoError(t, Unmarshal([]byte("{ n: 42 }"), &got))
	require.NotNil(t, got.N)
	assert.Equal(t, 42, *got.N)

	var nilOne Foo
	require.NoError(t, Unmarshal([]byte("{ n: null }"), &nilOne))
	assert.Nil(t, nilOne.N)
}

func TestUnmarshalTime(t *testing.T) {
	var got time.Time
	require.NoError(t, Unmarshal([]byte("'2026-05-20T15:04:05Z'"), &got))
	assert.Equal(t, time.Date(2026, 5, 20, 15, 4, 5, 0, time.UTC), got)
}

func TestUnmarshalDuration(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want time.Duration
	}{
		{name: "seconds", in: "'30s'", want: 30 * time.Second},
		{name: "microseconds", in: "'711.785us'", want: 711785 * time.Nanosecond},
		{name: "hours-and-seconds", in: "'1h0m30s'", want: time.Hour + 30*time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got time.Duration
			require.NoError(t, Unmarshal([]byte(tt.in), &got))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUnmarshalBytes(t *testing.T) {
	var got []byte
	require.NoError(t, Unmarshal([]byte("'AQID'"), &got))
	assert.Equal(t, []byte{0x01, 0x02, 0x03}, got)
}

type tagWrap struct {
	tag string
}

func (t *tagWrap) UnmarshalUB(data []byte) error {
	t.tag = string(data)
	return nil
}

type erroringUnmarshaler struct{}

func (*erroringUnmarshaler) UnmarshalUB([]byte) error {
	return errors.New("boom")
}

func TestUnmarshalerInterface(t *testing.T) {
	var got tagWrap
	require.NoError(t, Unmarshal([]byte("'whatever'"), &got))
	assert.Equal(t, "'whatever'", got.tag)
}

func TestUnmarshalerError(t *testing.T) {
	var got erroringUnmarshaler
	err := Unmarshal([]byte("'x'"), &got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestUnmarshalRequiresPointer(t *testing.T) {
	var n int
	err := Unmarshal([]byte("42"), n)
	require.Error(t, err)
}

func TestUnmarshalNilPointer(t *testing.T) {
	var p *int
	err := Unmarshal([]byte("42"), p)
	require.Error(t, err)
}

func TestUnmarshalTypeMismatch(t *testing.T) {
	var s string
	err := Unmarshal([]byte("42"), &s)
	require.Error(t, err)

	var n int
	err = Unmarshal([]byte("'hello'"), &n)
	require.Error(t, err)

	var f int
	err = Unmarshal([]byte("3.5"), &f)
	require.Error(t, err)
}

func TestUnmarshalInvalidInput(t *testing.T) {
	var got any
	err := Unmarshal([]byte("{ a: }"), &got)
	require.Error(t, err)
}

func TestUnmarshalEmptyInput(t *testing.T) {
	var got any
	err := Unmarshal([]byte(""), &got)
	require.Error(t, err)
}

func TestUnmarshalNumericWidths(t *testing.T) {
	var i8 int8
	require.NoError(t, Unmarshal([]byte("100"), &i8))
	assert.Equal(t, int8(100), i8)

	var i16 int16
	require.NoError(t, Unmarshal([]byte("30000"), &i16))
	assert.Equal(t, int16(30000), i16)

	var i32 int32
	require.NoError(t, Unmarshal([]byte("2000000000"), &i32))
	assert.Equal(t, int32(2000000000), i32)

	var u uint
	require.NoError(t, Unmarshal([]byte("42"), &u))
	assert.Equal(t, uint(42), u)

	var u8 uint8
	require.NoError(t, Unmarshal([]byte("200"), &u8))
	assert.Equal(t, uint8(200), u8)

	var u16 uint16
	require.NoError(t, Unmarshal([]byte("60000"), &u16))
	assert.Equal(t, uint16(60000), u16)

	var u32 uint32
	require.NoError(t, Unmarshal([]byte("4000000000"), &u32))
	assert.Equal(t, uint32(4000000000), u32)

	var u64 uint64
	require.NoError(t, Unmarshal([]byte("9000000000"), &u64))
	assert.Equal(t, uint64(9000000000), u64)

	var f32 float32
	require.NoError(t, Unmarshal([]byte("3.5"), &f32))
	assert.Equal(t, float32(3.5), f32)

	var f32FromInt float32
	require.NoError(t, Unmarshal([]byte("42"), &f32FromInt))
	assert.Equal(t, float32(42), f32FromInt)
}

func TestUnmarshalNumericOverflow(t *testing.T) {
	var i8 int8
	require.Error(t, Unmarshal([]byte("200"), &i8))

	var i16 int16
	require.Error(t, Unmarshal([]byte("40000"), &i16))

	var u8 uint8
	require.Error(t, Unmarshal([]byte("300"), &u8))

	var u32 uint32
	require.Error(t, Unmarshal([]byte("99999999999"), &u32))
}

func TestUnmarshalNegativeIntoUnsigned(t *testing.T) {
	var u uint
	require.Error(t, Unmarshal([]byte("-1"), &u))

	var u8 uint8
	require.Error(t, Unmarshal([]byte("-5"), &u8))
}

func TestUnmarshalQuotedMapKey(t *testing.T) {
	var m map[string]int
	require.NoError(t, Unmarshal([]byte("{ 'has space': 1, 'dot.path': 2 }"), &m))
	assert.Equal(t, map[string]int{"has space": 1, "dot.path": 2}, m)
}

type stringWrap struct {
	raw string
}

func (s *stringWrap) UnmarshalUB(data []byte) error {
	s.raw = "wrapped:" + string(data)
	return nil
}

func TestUnmarshalerInStructField(t *testing.T) {
	type outer struct {
		Name  string
		Inner stringWrap
	}
	var got outer
	require.NoError(t, Unmarshal([]byte("{ name: 'outside', inner: 'hi' }"), &got))
	assert.Equal(t, "outside", got.Name)
	assert.Equal(t, "wrapped:'hi'", got.Inner.raw)
}

func TestUnmarshalerInPointerStructField(t *testing.T) {
	type outer struct {
		Inner *stringWrap
	}
	var got outer
	require.NoError(t, Unmarshal([]byte("{ inner: 'hi' }"), &got))
	require.NotNil(t, got.Inner)
	assert.Equal(t, "wrapped:'hi'", got.Inner.raw)
}

func TestUnmarshalerInMapValue(t *testing.T) {
	var got map[string]stringWrap
	require.NoError(t, Unmarshal([]byte("{ a: 'x', b: 'y' }"), &got))
	assert.Equal(t, "wrapped:'x'", got["a"].raw)
	assert.Equal(t, "wrapped:'y'", got["b"].raw)
}

func TestUnmarshalerInSliceElement(t *testing.T) {
	var got []stringWrap
	require.NoError(t, Unmarshal([]byte("['x', 'y']"), &got))
	require.Len(t, got, 2)
	assert.Equal(t, "wrapped:'x'", got[0].raw)
	assert.Equal(t, "wrapped:'y'", got[1].raw)
}

func TestUnmarshalerDeeplyNested(t *testing.T) {
	type level2 struct {
		Tag stringWrap
	}
	type level1 struct {
		Inner level2
	}
	var got level1
	require.NoError(t, Unmarshal([]byte("{ inner: { tag: 'deep' } }"), &got))
	assert.Equal(t, "wrapped:'deep'", got.Inner.Tag.raw)
}

func TestUnmarshalFixedArray(t *testing.T) {
	var exact [3]int
	require.NoError(t, Unmarshal([]byte("[1, 2, 3]"), &exact))
	assert.Equal(t, [3]int{1, 2, 3}, exact)

	var larger [5]int
	require.NoError(t, Unmarshal([]byte("[7, 8]"), &larger))
	assert.Equal(t, [5]int{7, 8, 0, 0, 0}, larger)

	var smaller [2]int
	require.Error(t, Unmarshal([]byte("[1, 2, 3]"), &smaller))
}

func TestRoundTrip(t *testing.T) {
	type Inner struct {
		Name string
		When time.Time
	}
	type Outer struct {
		Tag    string
		Sizes  []int
		Tags   map[string]string
		In     Inner
		MaxAge time.Duration
	}
	original := Outer{
		Tag:    "web",
		Sizes:  []int{1, 2, 3},
		Tags:   map[string]string{"env": "prod"},
		In:     Inner{Name: "alpha", When: time.Date(2026, 5, 20, 15, 4, 5, 0, time.UTC)},
		MaxAge: 90 * time.Second,
	}
	encoded, err := Marshal(original)
	require.NoError(t, err)
	var decoded Outer
	require.NoError(t, Unmarshal(encoded, &decoded))
	assert.Equal(t, original, decoded)
}
