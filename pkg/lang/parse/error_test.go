package parse

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrorString(t *testing.T) {
	pos := Position{File: "main.ub", Line: 7, Column: 3}
	e := Errorf(ErrType, pos, "expected %s, got %s", "string", "integer")
	require.Equal(t, "main.ub:7:3: type: expected string, got integer", e.Error())
}

func TestErrorWithHint(t *testing.T) {
	pos := Position{Line: 1, Column: 1}
	e := &Error{
		Kind: ErrSchema, Pos: pos, Msg: "unknown block 'foo'",
		Hint: "valid blocks: inputs, resources, ...",
	}
	got := e.Error()
	require.Contains(t, got, "1:1: schema: unknown block 'foo'")
	require.Contains(t, got, "hint: valid blocks")
}

func TestErrorListBudget(t *testing.T) {
	l := NewErrorList(2)
	l.Addf(ErrParse, Position{Line: 1}, "first")
	l.Addf(ErrParse, Position{Line: 2}, "second")
	l.Addf(ErrParse, Position{Line: 3}, "dropped - over budget")
	require.Equal(t, 2, l.Len())
}

func TestErrorListSortOrder(t *testing.T) {
	l := NewErrorList(0)
	l.Addf(ErrParse, Position{File: "b.ub", Line: 1, Column: 1}, "b")
	l.Addf(ErrParse, Position{File: "a.ub", Line: 5, Column: 1}, "a-late")
	l.Addf(ErrParse, Position{File: "a.ub", Line: 1, Column: 7}, "a-early-col")
	l.Addf(ErrParse, Position{File: "a.ub", Line: 1, Column: 1}, "a-earliest")

	got := l.Errors()
	want := []string{"a-earliest", "a-early-col", "a-late", "b"}
	require.Len(t, got, len(want))
	for i, w := range want {
		require.Equal(t, w, got[i].Msg, "position %d", i)
	}
}

func TestErrorListErr(t *testing.T) {
	l := NewErrorList(0)
	require.Nil(t, l.Err())

	l.Addf(ErrType, Position{}, "one")
	require.IsType(t, &Error{}, l.Err())

	l.Addf(ErrType, Position{}, "two")
	require.IsType(t, &ErrorList{}, l.Err())
}

func TestErrorKindString(t *testing.T) {
	cases := map[ErrorKind]string{
		ErrUnknown: "error",
		ErrParse:   "parse",
		ErrLex:     "lex",
		ErrSchema:  "schema",
		ErrType:    "type",
		ErrResolve: "resolve",
	}
	for k, want := range cases {
		require.Equal(t, want, k.String(), "kind %d", k)
	}
}
