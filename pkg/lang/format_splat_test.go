package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func splatFormatCases() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{name: "splat then field", src: "x: var.subnets[*].id\n", want: "x: var.subnets[*].id\n"},
		{
			name: "node output field",
			src:  "x: resource.aws.vpc.main.subnets[*].cidr\n",
			want: "x: resource.aws.vpc.main.subnets[*].cidr\n",
		},
		{name: "nested splat", src: "x: var.grid[*][*].name\n", want: "x: var.grid[*][*].name\n"},
		{
			name: "index then splat then field",
			src:  "x: var.matrix[0][*].id\n",
			want: "x: var.matrix[0][*].id\n",
		},
		{name: "splat then index", src: "x: var.matrix[*][0]\n", want: "x: var.matrix[*][0]\n"},
		{
			name: "string index then splat",
			src:  "x: var.m['key'].items[*].id\n",
			want: "x: var.m['key'].items[*].id\n",
		},
		{name: "bare splat round-trips", src: "x: var.list[*]\n", want: "x: var.list[*]\n"},
		{name: "spaces normalized", src: "x: var.list[ * ].id\n", want: "x: var.list[*].id\n"},
		{
			name: "splat as a call argument",
			src:  "x: core.format('%s', var.subnets[*].id)\n",
			want: "x: core.format('%s', var.subnets[*].id)\n",
		},
	}
}

func TestFormatSplat(t *testing.T) {
	for _, c := range splatFormatCases() {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, formatString(t, c.src))
		})
	}
}

func TestFormatSplatDeterministic(t *testing.T) {
	for _, c := range splatFormatCases() {
		t.Run(c.name, func(t *testing.T) {
			first := formatString(t, c.src)
			for range 5 {
				require.Equal(t, first, formatString(t, c.src))
			}
		})
	}
}
