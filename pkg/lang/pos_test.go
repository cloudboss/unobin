package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPositionString(t *testing.T) {
	cases := []struct {
		name string
		pos  Position
		want string
	}{
		{"with file", Position{File: "stack.ub", Line: 12, Column: 4}, "stack.ub:12:4"},
		{"without file", Position{Line: 12, Column: 4}, "12:4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, c.pos.String())
		})
	}
}

func TestPositionIsZero(t *testing.T) {
	require.True(t, (Position{}).IsZero())
	require.False(t, (Position{Line: 1}).IsZero())
	require.False(t, (Position{File: "x"}).IsZero())
}
