package lang

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// interpRepr renders an interpolated string's parts: literals verbatim, a
// slot as <S> (or <S:verb>). Used to compare a value before and after a
// format round trip without depending on the runtime evaluator.
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

func TestFormatInterpolatedIdempotent(t *testing.T) {
	srcs := []string{
		"x: $'hello'\n",
		"x: $''\n",
		"x: $'{{var.region}}'\n",
		"x: $'cluster-{{var.name}}-prod'\n",
		"x: $'{{var.region}}/{{var.zone}}'\n",
		"x: $'{{var.size:%03d}}'\n",
		"x: $'{{ var.size : %-10s }}'\n",
		"x: $'{{resource.aws.vpc.main.id}}'\n",
		"x: $'{{format('%s', var.x)}}'\n",
		`x: $'it\'s {{var.x}}'` + "\n",
		`x: $'a \{{ b }} c'` + "\n",
		`x: $'path\\to\\thing {{var.x}}'` + "\n",
	}
	for _, src := range srcs {
		t.Run(src, func(t *testing.T) {
			once := formatString(t, src)
			require.Equal(t, once, formatString(t, once), "format should be idempotent")
		})
	}
}

func TestFormatInterpolatedNormalizes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"adds slot padding", "x: $'{{var.region}}'\n", "x: $'{{ var.region }}'\n"},
		{"collapses extra spaces", "x: $'{{   var.region   }}'\n", "x: $'{{ var.region }}'\n"},
		{"tightens verb colon", "x: $'{{ var.size : %03d }}'\n", "x: $'{{ var.size:%03d }}'\n"},
		{"keeps literal around slot", "x: $'a-{{var.x}}-b'\n", "x: $'a-{{ var.x }}-b'\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, formatString(t, tt.in))
		})
	}
}

func TestFormatInterpolatedTripleRoundTrip(t *testing.T) {
	// Each form re-parses to the same form and the same value parts, and
	// formatting is idempotent.
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

func TestFormatInterpolatedWrapsToTriple(t *testing.T) {
	opts := FormatOptions{MaxColumn: 60, WrapStrings: true}

	t.Run("spaces fold", func(t *testing.T) {
		src := "x: $'deploying {{ var.cluster-name }} to region " +
			"{{ var.region }} for tenant {{ var.tenant-id }} now'\n"
		f, err := ParseSource("t.ub", []byte(src))
		require.NoError(t, err)
		out, err := FormatWith(f, opts)
		require.NoError(t, err)
		require.Contains(t, string(out), "$'''>", "folds when literals have spaces")
		after := parseField0(t, string(out))
		require.Equal(t,
			"deploying <S> to region <S> for tenant <S> now", interpRepr(after))
	})

	t.Run("no spaces join", func(t *testing.T) {
		src := "x: $'https://{{ var.host }}/api/v1/{{ var.namespace }}/" +
			"{{ var.resource }}/{{ var.id }}'\n"
		f, err := ParseSource("t.ub", []byte(src))
		require.NoError(t, err)
		out, err := FormatWith(f, opts)
		require.NoError(t, err)
		require.Contains(t, string(out), "$'''\\", "joins when literals have no spaces")
		after := parseField0(t, string(out))
		require.Equal(t,
			"https://<S>/api/v1/<S>/<S>/<S>", interpRepr(after))
	})
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
