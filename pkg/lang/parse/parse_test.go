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

func TestParseStringEscapes(t *testing.T) {
	cases := map[string]string{
		`s: 'plain'`:           "plain",
		`s: 'it\'s'`:           "it's",
		`s: 'a\\b'`:            `a\b`,
		`s: 'line1\nline2'`:    "line1\nline2",
		`s: 'tab\there'`:       "tab\there",
		`s: 'unknown\xescape'`: `unknown\xescape`,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			f := mustParse(t, src)
			require.Equal(t, want, f.Body.Fields[0].Value.(*StringLit).Value)
		})
	}
}

func TestParsePosition(t *testing.T) {
	src := "name: 'cfer'\nother: 1\n"
	f, err := ParseSource("stack.ub", []byte(src))
	require.NoError(t, err)

	first := f.Body.Fields[0].S.Start
	require.Equal(t, "stack.ub", first.File)
	require.Equal(t, 1, first.Line)
	require.Equal(t, 1, first.Column)

	second := f.Body.Fields[1].S.Start
	require.Equal(t, "stack.ub", second.File)
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
