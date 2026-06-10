package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOperatorSpacingForms pins which operator spacings parse and as
// what: tight forms are one name (hyphen after an identifier) or
// arithmetic (every other pairing), evenly spaced forms are
// arithmetic, and half-spaced forms are errors.
func TestOperatorSpacingForms(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"tight hyphen after ident is a name", "x-1", "ident"},
		{"tight hyphen after number subtracts", "5-1", "infix"},
		{"tight plus", "x+1", "infix"},
		{"tight comparison", "x==2", "infix"},
		{"spaced minus", "x - 1", "infix"},
		{"newline after spaced operator", "x -\n1", "infix"},
		{"unary minus at value position", "-1", "number"},
		{"unary minus after comma", "[1, -2]", "array"},
		{"half minus glued left", "x- 1", "error"},
		{"half minus glued right", "x -1", "error"},
		{"half plus glued left", "x+ 1", "error"},
		{"half comparison glued right", "x ==2", "error"},
		{"half coalesce glued right", "x ??y", "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := ParseSource("test.ub", []byte("a: "+tt.src+"\n"))
			if tt.want == "error" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			switch tt.want {
			case "ident":
				require.IsType(t, &Ident{}, f.Body.Fields[0].Value)
			case "infix":
				require.IsType(t, &Infix{}, f.Body.Fields[0].Value)
			case "number":
				require.IsType(t, &NumberLit{}, f.Body.Fields[0].Value)
			case "array":
				arr := f.Body.Fields[0].Value.(*ArrayLit)
				require.Len(t, arr.Elements, 2)
			}
		})
	}
}

func TestParseExprAtoms(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want any
	}{
		{name: "true", in: "true", want: true},
		{name: "false", in: "false", want: false},
		{name: "null", in: "null"},
		{name: "int", in: "42", want: int64(42)},
		{name: "negative", in: "-7", want: int64(-7)},
		{name: "float", in: "3.5", want: 3.5},
		{name: "string", in: "'hello'", want: "hello"},
		{name: "string with escape", in: `'a\nb'`, want: "a\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseExpr("", []byte(tt.in))
			require.NoError(t, err)
			switch want := tt.want.(type) {
			case bool:
				assert.Equal(t, want, got.(*BoolLit).Value)
			case int64:
				assert.Equal(t, want, got.(*NumberLit).ParsedInt)
			case float64:
				assert.Equal(t, want, got.(*NumberLit).ParsedFloat)
			case string:
				assert.Equal(t, want, got.(*StringLit).Value)
			default:
				assert.IsType(t, (*NullLit)(nil), got)
			}
		})
	}
}

func TestParseExprList(t *testing.T) {
	got, err := ParseExpr("", []byte("[1, 'two', true, null]"))
	require.NoError(t, err)
	arr, ok := got.(*ArrayLit)
	require.True(t, ok)
	require.Len(t, arr.Elements, 4)
}

func TestParseExprMap(t *testing.T) {
	got, err := ParseExpr("", []byte("{ a: 1, b: [2, 3] }"))
	require.NoError(t, err)
	obj, ok := got.(*ObjectLit)
	require.True(t, ok)
	require.Len(t, obj.Fields, 2)
	assert.Equal(t, "a", obj.Fields[0].Key.Name)
	assert.Equal(t, "b", obj.Fields[1].Key.Name)
}

func TestParseExprNested(t *testing.T) {
	in := `{ name: 'web', tags: { Name: 'thing' }, sizes: [1, 2] }`
	got, err := ParseExpr("", []byte(in))
	require.NoError(t, err)
	require.IsType(t, (*ObjectLit)(nil), got)
}

func TestParseExprEmpty(t *testing.T) {
	_, err := ParseExpr("", []byte(""))
	require.Error(t, err)
}

func TestParseExprTrailingContent(t *testing.T) {
	_, err := ParseExpr("", []byte("42 trailing"))
	require.Error(t, err)
}

func TestParseExprInvalid(t *testing.T) {
	_, err := ParseExpr("", []byte("{ a: }"))
	require.Error(t, err)
}
