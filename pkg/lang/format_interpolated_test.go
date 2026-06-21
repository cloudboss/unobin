package lang

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// interpRepr renders an interpolated string's parts: literals verbatim and a
// slot as <S> or <S:verb>.
func interpRepr(is *InterpolatedString) string {
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

func parseField0(t *testing.T, src string) *InterpolatedString {
	t.Helper()
	f, err := ParseSource("t.ub", []byte(src))
	require.NoError(t, err)
	is, ok := f.Body.Fields[0].Value.(*InterpolatedString)
	require.True(t, ok, "value is %T, want *InterpolatedString", f.Body.Fields[0].Value)
	return is
}

func TestFormatInterpolatedTripleEncodeDecode(t *testing.T) {
	// Each form re-parses to the same form and the same value parts.
	srcs := []string{
		`$'''Hello {{ var.name }}!'''`,
		`$'''id-{{ var.n:%03d }}'''`,
		"$'''>\n  Greeting {{ var.name }} in\n  {{ var.region }}.\n  '''",
		"$'''>-\n  {{ var.a }} and {{ var.b }} together\n  '''",
		"$'''|-\n  echo {{ var.msg }}\n  exit {{ var.code }}\n  '''",
		"$'''\\-\n  https://{{ var.host }}\n  /v1/{{ var.id }}\n  '''",
	}
	for _, src := range srcs {
		t.Run(src, func(t *testing.T) {
			in := "x: " + src + "\n"
			before := parseField0(t, in)
			out := formatString(t, in)
			after := parseField0(t, out)
			require.Equal(t, before.Form, after.Form, "form preserved")
			require.Equal(t, interpRepr(before), interpRepr(after), "value preserved")
			require.Equal(t, out, formatString(t, out), "idempotent")
		})
	}
}

func TestFormatInterpolatedReparses(t *testing.T) {
	// The formatted output parses back to an interpolated string with the
	// same literal/slot layout.
	f, err := ParseSource("test.ub", []byte("x: $'a-{{var.x}}-{{var.y:%d}}'\n"))
	require.NoError(t, err)
	out := formatString(t, "x: $'a-{{var.x}}-{{var.y:%d}}'\n")

	g, err := ParseSource("test.ub", []byte(out))
	require.NoError(t, err)
	orig := f.Body.Fields[0].Value.(*InterpolatedString)
	round := g.Body.Fields[0].Value.(*InterpolatedString)
	require.Len(t, round.Parts, len(orig.Parts))
	for i := range orig.Parts {
		require.Equal(t, orig.Parts[i].Lit, round.Parts[i].Lit, "part %d literal", i)
		require.Equal(t, orig.Parts[i].Verb, round.Parts[i].Verb, "part %d verb", i)
		require.Equal(t, orig.Parts[i].Expr == nil, round.Parts[i].Expr == nil, "part %d slot", i)
	}
}
