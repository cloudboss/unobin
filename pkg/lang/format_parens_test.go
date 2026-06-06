package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The parser consumes grouping parens, so the formatter must put back
// every pair the grammar needs to rebuild the same tree: a looser
// operator under a tighter one, equal binding on the right of a
// left-folding ladder, and conditionals anywhere the grammar admits
// only an operator chain.
func TestFormatKeepsRequiredParens(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"coalesce under comparison", "v: a <= (b ?? c)\n"},
		{"coalesce under arithmetic", "v: (a ?? b) + 1\n"},
		{"equal precedence right of minus", "v: a - (b - c)\n"},
		{"equal precedence right of divide", "v: a / (b * c)\n"},
		{"plus under times", "v: (a + b) * c\n"},
		{"or under and", "v: a && (b || c)\n"},
		{"and right of and", "v: a && (b && c)\n"},
		{"coalesce right of coalesce", "v: a ?? (b ?? c)\n"},
		{"equality under comparison", "v: (a == b) < c\n"},
		{"not over and", "v: !(a && b)\n"},
		{"negate over plus", "v: -(a + b)\n"},
		{"conditional left of plus", "v: (if c then a else b) + 1\n"},
		{"conditional right of coalesce", "v: x ?? (if c then a else b)\n"},
		{"conditional as a condition", "v: if (if a then b else c) then d else e\n"},
		{"conditional as list source", "v: [ for x in (if c then xs else ys) : x ]\n"},
		{"conditional as map source", "v: { for k in (if c then m else n) : k => 1 }\n"},
		{"conditional as map key", "v: { for k in m : (if c then k else 'x') => 1 }\n"},
		{"conditional in a filter", "v: [ for x in xs : x when (if c then a else b) ]\n"},
		{"parens inside interpolation", "v: $'{{ a - (b - c) }}'\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.src, formatString(t, c.src))
		})
	}
	// A second pass over each case proves the output is a fixed point.
	for _, c := range cases {
		t.Run(c.name+" idempotent", func(t *testing.T) {
			once := formatString(t, c.src)
			require.Equal(t, once, formatString(t, once))
		})
	}
}

// Parens the grammar rebuilds on its own are noise and go away.
func TestFormatRemovesRedundantParens(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"left fold of plus", "v: (a + b) + c\n", "v: a + b + c\n"},
		{"tighter left of plus", "v: (a * b) + c\n", "v: a * b + c\n"},
		{"tighter right of plus", "v: a + (b * c)\n", "v: a + b * c\n"},
		{"around a primary", "v: ((a))\n", "v: a\n"},
		{"left fold of coalesce", "v: (a ?? b) ?? c\n", "v: a ?? b ?? c\n"},
		{"comparison under equality", "v: (a < b) == c\n", "v: a < b == c\n"},
		{"around a then branch", "v: if c then (a + b) else b\n", "v: if c then a + b else b\n"},
		{"around a call argument", "v: f.g((a + b))\n", "v: f.g(a + b)\n"},
		{"around an index", "v: xs[(a + b)]\n", "v: xs[a + b]\n"},
		{"tighter under not", "v: !(a)\n", "v: !a\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, formatString(t, c.src))
		})
	}
	for _, c := range cases {
		t.Run(c.name+" idempotent", func(t *testing.T) {
			once := formatString(t, c.src)
			require.Equal(t, once, formatString(t, once))
		})
	}
}
