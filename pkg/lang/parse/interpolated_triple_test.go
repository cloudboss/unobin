package parse

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// reprParts renders the parts of an interpolated string: literal runs
// verbatim, a slot as <S> (or <S:verb> when it carries a printf verb).
func reprParts(is *InterpolatedString) string {
	var b strings.Builder
	for _, p := range is.Parts {
		switch {
		case p.Expr == nil:
			b.WriteString(p.Lit)
		case p.Verb != "":
			b.WriteString("<S:" + p.Verb + ">")
		default:
			b.WriteString("<S>")
		}
	}
	return b.String()
}

func TestInterpolatedTripleForms(t *testing.T) {
	tests := []struct {
		name string
		src  string
		form StringForm
		repr string
	}{
		{
			"single line",
			`$'''Hello {{ input.name }}!'''`,
			StringTripleQuoteSingleLine,
			"Hello <S>!",
		},
		{
			"single line verb",
			`$'''id-{{ input.n:%03d }}'''`,
			StringTripleQuoteSingleLine,
			"id-<S:%03d>",
		},
		{
			"single line escaped brace",
			`$'''raw \{{ not a slot }} {{ input.x }}'''`,
			StringTripleQuoteSingleLine,
			"raw {{ not a slot }} <S>",
		},
		{
			"folded clip",
			"$'''>\n  Hello {{ input.name }},\n  welcome.\n  '''",
			StringFoldedClip,
			"Hello <S>, welcome.\n",
		},
		{
			"folded strip two slots",
			"$'''>-\n  {{ input.a }} and\n  {{ input.b }}\n  '''",
			StringFoldedStrip,
			"<S> and <S>",
		},
		{
			"literal strip",
			"$'''|-\n  echo {{ input.msg }}\n  exit {{ input.code }}\n  '''",
			StringLiteralStrip,
			"echo <S>\nexit <S>",
		},
		{
			"joined strip",
			"$'''\\-\n  https://{{ input.host }}\n  /v1/{{ input.id }}\n  '''",
			StringJoinedStrip,
			"https://<S>/v1/<S>",
		},
		{
			"slot with call and nested string",
			`$'''x={{ format('%s', input.x) }}'''`,
			StringTripleQuoteSingleLine,
			"x=<S>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := interpolatedString(t, tt.src)
			require.Equal(t, tt.form, is.Form, "form")
			require.Equal(t, tt.repr, reprParts(is), "parts")
		})
	}
}

func TestInterpolatedTripleSlotExpr(t *testing.T) {
	is := interpolatedString(t, "$'''>\n  region {{ input.region }}\n  '''")
	// parts: "region ", slot(input.region), "\n"
	require.Len(t, is.Parts, 3)
	dp, ok := is.Parts[1].Expr.(*DotPath)
	require.True(t, ok, "want *DotPath, got %T", is.Parts[1].Expr)
	require.Equal(t, "input", dp.Root.Name)
	require.Equal(t, "region", dp.Segments[0].Name)
}

func TestInterpolatedTripleInvalid(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"slot spans newline", "x: $'''|\n  {{ input.a\n  + input.b }}\n  '''\n"},
		{"escaped close brace", `x: $'''oops \}} here'''` + "\n"},
		{"bad verb", `x: $'''{{ input.x:nope }}'''` + "\n"},
		{"unterminated slot", `x: $'''{{ input.x'''` + "\n"},
		{"empty slot", `x: $'''{{}}'''` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSource("test.ub", []byte(tt.src))
			require.Error(t, err)
		})
	}
}
