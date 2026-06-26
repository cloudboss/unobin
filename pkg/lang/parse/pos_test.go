package parse

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
		{"with file", Position{File: "factory.ub", Line: 12, Column: 4}, "factory.ub:12:4"},
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

func TestLineStarts(t *testing.T) {
	require.Equal(t, []int{0, 2, 5}, LineStarts([]byte("a\nbc\n")))
}

func TestSourceFilePosition(t *testing.T) {
	src := []byte("a\nbc\n")
	lineStarts := LineStarts(src)
	file := NewSourceFile("factory.ub", lineStarts)
	lineStarts[1] = 99

	require.Equal(t, Position{File: "factory.ub", Line: 1, Column: 1, Offset: 0}, file.Position(0))
	require.Equal(t, Position{File: "factory.ub", Line: 1, Column: 2, Offset: 1}, file.Position(1))
	require.Equal(t, Position{File: "factory.ub", Line: 2, Column: 1, Offset: 2}, file.Position(2))
	require.Equal(t,
		Position{File: "factory.ub", Line: 3, Column: 1, Offset: 5},
		file.Position(len(src)))
}

func TestSourceFileSpan(t *testing.T) {
	file := NewSourceFile("factory.ub", LineStarts([]byte("a\nbc\n")))
	span := file.Span(1, 4)

	require.Equal(t, Position{File: "factory.ub", Line: 1, Column: 2, Offset: 1}, span.Start)
	require.Equal(t, Position{File: "factory.ub", Line: 2, Column: 3, Offset: 4}, span.End)
}
