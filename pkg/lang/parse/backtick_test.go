package parse

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBacktickSigilDispatch(t *testing.T) {
	tests := []struct {
		name string
		src  string
		val  string
		form StringForm
	}{
		{
			name: "literal clip",
			src:  "k: `|\n  one\n  two\n  `\n",
			val:  "one\ntwo\n",
			form: StringLiteralClip,
		},
		{
			name: "literal strip",
			src:  "k: `|-\n  one\n  two\n  `\n",
			val:  "one\ntwo",
			form: StringLiteralStrip,
		},
		{
			name: "folded clip",
			src:  "k: `>\n  one\n  two\n  `\n",
			val:  "one two\n",
			form: StringFoldedClip,
		},
		{
			name: "folded strip",
			src:  "k: `>-\n  one\n  two\n  `\n",
			val:  "one two",
			form: StringFoldedStrip,
		},
		{
			name: "folded paragraph break",
			src:  "k: `>\n  one\n\n  two\n  `\n",
			val:  "one\ntwo\n",
			form: StringFoldedClip,
		},
		{
			name: "folded double blank",
			src:  "k: `>\n  one\n\n\n  two\n  `\n",
			val:  "one\n\ntwo\n",
			form: StringFoldedClip,
		},
		{
			name: "folded more indented preserved",
			src:  "k: `>\n  prose\n    code\n    more\n  back\n  `\n",
			val:  "prose\n  code\n  more\nback\n",
			form: StringFoldedClip,
		},
		{
			name: "joined clip",
			src:  "k: `\\\n  https://example.com/\n  api/v1/users\n  `\n",
			val:  "https://example.com/api/v1/users\n",
			form: StringJoinedClip,
		},
		{
			name: "joined strip",
			src:  "k: `\\-\n  https://example.com/\n  api/v1/users\n  `\n",
			val:  "https://example.com/api/v1/users",
			form: StringJoinedStrip,
		},
		{
			name: "joined ignores blank lines",
			src:  "k: `\\-\n  a\n\n  b\n  `\n",
			val:  "ab",
			form: StringJoinedStrip,
		},
		{
			name: "single-line backtick",
			src:  "k: `hello world`\n",
			val:  "hello world",
			form: StringBacktickSingleLine,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := ParseSource("test.ub", []byte(tt.src))
			require.NoError(t, err)
			require.Len(t, f.Body.Fields, 1)
			s := f.Body.Fields[0].Value.(*StringLit)
			require.Equal(t, tt.form, s.Form, "form")
			require.Equal(t, tt.val, s.Value, "value")
		})
	}
}

func TestBacktickEmbeddedBacktick(t *testing.T) {
	src := "k: `|\n  before `tick` after\n  `\n"
	f, err := ParseSource("test.ub", []byte(src))
	require.NoError(t, err)
	require.Len(t, f.Body.Fields, 1)
	s := f.Body.Fields[0].Value.(*StringLit)
	require.Equal(t, "before `tick` after\n", s.Value)
}
