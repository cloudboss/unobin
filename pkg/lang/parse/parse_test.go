package parse

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func mustParse(t *testing.T, src string) *File {
	t.Helper()
	f, err := ParseSource("test.ub", []byte(src))
	require.NoError(t, err, "parse failed for source:\n%s", src)
	return f
}

func TestParseStringEscapesValid(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"plain", `s: 'plain'`, "plain"},
		{"empty", `s: ''`, ""},
		{"no backslash with quote-ish text", `s: 'a:b/c=d'`, "a:b/c=d"},
		{"escaped quote", `s: 'it\'s'`, "it's"},
		{"only an escaped quote", `s: '\''`, "'"},
		{"two escaped quotes", `s: '\'\''`, "''"},
		{"backslash", `s: 'a\\b'`, `a\b`},
		{"only a backslash", `s: '\\'`, `\`},
		{"double backslash", `s: '\\\\'`, `\\`},
		{"backslash then n is not newline", `s: 'a\\nb'`, `a\nb`},
		{"backslash before quote", `s: 'a\\'`, `a\`},
		{"backslash then escaped quote", `s: '\\\''`, `\'`},
		{"newline", `s: 'line1\nline2'`, "line1\nline2"},
		{"tab", `s: 'tab\there'`, "tab\there"},
		{"carriage return", `s: 'ret\rhere'`, "ret\rhere"},
		{"nul", `s: 'nul\0byte'`, "nul\x00byte"},
		{"escape at start", `s: '\nfoo'`, "\nfoo"},
		{"escape at end", `s: 'foo\n'`, "foo\n"},
		{"adjacent escapes", `s: '\n\t\r'`, "\n\t\r"},
		{"mixed escapes", `s: 'a\nb\tc\\d\'e'`, "a\nb\tc\\d'e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := mustParse(t, tt.src)
			require.Equal(t, tt.want, f.Body.Fields[0].Value.(*StringLit).Value)
		})
	}
}

func TestParseStringEscapesInvalid(t *testing.T) {
	// Strict escaping: any escape outside the recognized set (\\ \' \n \t
	// \r \0) is a parse error rather than being kept literal. Backslash-
	// heavy values use a raw triple-quoted string instead.
	tests := []struct {
		name string
		src  string
	}{
		{"unknown letter x", `s: 'unknown\xescape'`},
		{"unknown letter d", `s: 'digit\d'`},
		{"single open brace", `s: 'brace\{'`},
		{"single close brace", `s: 'brace\}'`},
		{"bell a", `s: 'ring\a'`},
		{"backspace b", `s: 'back\b'`},
		{"form feed f", `s: 'feed\f'`},
		{"vertical tab v", `s: 'vtab\v'`},
		{"unicode u", `s: 'uni\uABCD'`},
		{"hex x with digits", `s: 'hex\x41'`},
		{"octal-ish 1", `s: 'oct\1'`},
		{"uppercase N is not newline", `s: 'big\Nope'`},
		{"escaped double quote", `s: 'dq\"here'`},
		{"backslash space", `s: 'sp\ here'`},
		{"escaped hash", `s: 'hash\#one'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSource("test.ub", []byte(tt.src))
			require.Error(t, err)
		})
	}
}

func TestParsePosition(t *testing.T) {
	src := "name: 'cfer'\nother: 1\n"
	f, err := ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)

	first := f.Body.Fields[0].S.Start
	require.Equal(t, "factory.ub", first.File)
	require.Equal(t, 1, first.Line)
	require.Equal(t, 1, first.Column)

	second := f.Body.Fields[1].S.Start
	require.Equal(t, "factory.ub", second.File)
	require.Equal(t, 2, second.Line)
	require.Equal(t, 1, second.Column)
}

func TestParseNestedPosition(t *testing.T) {
	src := "outer: {\n  inner: 'x'\n}\n"
	f := mustParse(t, src)
	inner := f.Body.Fields[0].Value.(*ObjectLit).Fields[0].S.Start
	require.Equal(t, 2, inner.Line)
	require.Equal(t, 3, inner.Column)
}

func TestParseAllowsConsecutiveHyphens(t *testing.T) {
	f := mustParse(t, "fine--name: 1")
	require.Equal(t, "fine--name", f.Body.Fields[0].Key.Name)
	require.Equal(t, int64(1), f.Body.Fields[0].Value.(*NumberLit).ParsedInt)
}

func TestParseCapturesComments(t *testing.T) {
	src := "# top\nname: 'cfer' # trailing\n# between\nx: 1\n# final"
	f := mustParse(t, src)
	require.Len(t, f.Comments, 4)

	require.Equal(t, "# top", f.Comments[0].Text)
	require.Equal(t, 1, f.Comments[0].S.Start.Line)
	require.Equal(t, 1, f.Comments[0].S.Start.Column)

	require.Equal(t, "# trailing", f.Comments[1].Text)
	require.Equal(t, 2, f.Comments[1].S.Start.Line)

	require.Equal(t, "# between", f.Comments[2].Text)
	require.Equal(t, 3, f.Comments[2].S.Start.Line)

	require.Equal(t, "# final", f.Comments[3].Text)
	require.Equal(t, 5, f.Comments[3].S.Start.Line)
}

func TestParseNoCommentsLeavesSliceNil(t *testing.T) {
	f := mustParse(t, "name: 'cfer'\n")
	require.Nil(t, f.Comments)
}

func TestParseCollectionsRecordEndPosition(t *testing.T) {
	f := mustParse(t, "outer: {\n  a: 1\n}\nlist: [\n  1,\n  2,\n]\n")

	obj := f.Body.Fields[0].Value.(*ObjectLit).Span()
	require.Equal(t, 1, obj.Start.Line)
	require.Equal(t, 8, obj.Start.Column)
	require.Equal(t, 3, obj.End.Line)
	require.Equal(t, 2, obj.End.Column)

	arr := f.Body.Fields[1].Value.(*ArrayLit).Span()
	require.Equal(t, 4, arr.Start.Line)
	require.Equal(t, 7, arr.End.Line)
	require.Equal(t, 2, arr.End.Column)
}
