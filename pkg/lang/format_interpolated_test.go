package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
