package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func guardedFormatCases() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{name: "guarded field", src: "x: var.tls?.port\n", want: "x: var.tls?.port\n"},
		{
			name: "guarded chain",
			src:  "x: var.cfg?.db?.host\n",
			want: "x: var.cfg?.db?.host\n",
		},
		{
			name: "guard after plain segments",
			src:  "x: resource.aws.vpc.main.logging?.bucket\n",
			want: "x: resource.aws.vpc.main.logging?.bucket\n",
		},
		{
			name: "guard then index",
			src:  "x: var.cfg?.hosts[0]\n",
			want: "x: var.cfg?.hosts[0]\n",
		},
		{
			name: "guard as a call argument",
			src:  "x: core.format('%s', var.cfg?.db)\n",
			want: "x: core.format('%s', var.cfg?.db)\n",
		},
	}
}

func TestFormatGuarded(t *testing.T) {
	for _, c := range guardedFormatCases() {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, formatString(t, c.src))
		})
	}
}

func TestFormatGuardedDeterministic(t *testing.T) {
	for _, c := range guardedFormatCases() {
		t.Run(c.name, func(t *testing.T) {
			first := formatString(t, c.src)
			for range 5 {
				require.Equal(t, first, formatString(t, c.src))
			}
		})
	}
}
