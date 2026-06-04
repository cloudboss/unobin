package runtime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMakeFuncUnary(t *testing.T) {
	ft := MakeFunc("all", "Report whether every boolean is true.",
		func(bools []bool) (bool, error) {
			for _, b := range bools {
				if !b {
					return false, nil
				}
			}
			return true, nil
		})
	require.Equal(t, "all", ft.Name)
	require.Equal(t, "Report whether every boolean is true.", ft.Description)
	require.Equal(t, 1, ft.ArgCount)
	require.False(t, ft.Variadic)

	got, err := ft.Func([]any{[]any{true, true}})
	require.NoError(t, err)
	require.Equal(t, true, got)

	got, err = ft.Func([]any{[]any{true, false}})
	require.NoError(t, err)
	require.Equal(t, false, got)

	got, err = ft.Func([]any{[]any{}})
	require.NoError(t, err)
	require.Equal(t, true, got)
}

func TestMakeFuncArgErrors(t *testing.T) {
	ft := MakeFunc("all", "d", func(bools []bool) (bool, error) { return true, nil })

	_, err := ft.Func([]any{"x"})
	require.EqualError(t, err, "all: argument 1 must be a list, got a string")

	_, err = ft.Func([]any{[]any{true, int64(1)}})
	require.EqualError(t, err, "all: argument 1: element 1 must be a boolean, got an integer")

	_, err = ft.Func([]any{nil})
	require.EqualError(t, err, "all: argument 1 must be a list, got null")

	_, err = ft.Func([]any{})
	require.EqualError(t, err, "all: expected 1 argument, got 0")

	_, err = ft.Func([]any{[]any{}, []any{}})
	require.EqualError(t, err, "all: expected 1 argument, got 2")
}

func TestMakeFuncScalarAndAny(t *testing.T) {
	upper := MakeFunc("shout", "d", func(s string) (string, error) { return s + "!", nil })
	got, err := upper.Func([]any{"hi"})
	require.NoError(t, err)
	require.Equal(t, "hi!", got)

	_, err = upper.Func([]any{int64(3)})
	require.EqualError(t, err, "shout: argument 1 must be a string, got an integer")

	length := MakeFunc("length", "d", func(v any) (int64, error) {
		switch x := v.(type) {
		case nil:
			return 0, nil
		case string:
			return int64(len(x)), nil
		case []any:
			return int64(len(x)), nil
		}
		return 0, fmt.Errorf("length: argument must be a string or list")
	})
	got, err = length.Func([]any{"abc"})
	require.NoError(t, err)
	require.Equal(t, int64(3), got)

	got, err = length.Func([]any{nil})
	require.NoError(t, err, "an any parameter accepts null")
	require.Equal(t, int64(0), got)
}

func TestMakeFuncMultipleFixedArgs(t *testing.T) {
	clamp := MakeFunc("clamp", "d", func(n, lo, hi int64) (int64, error) {
		if n < lo {
			return lo, nil
		}
		if n > hi {
			return hi, nil
		}
		return n, nil
	})
	require.Equal(t, 3, clamp.ArgCount)
	require.False(t, clamp.Variadic)

	got, err := clamp.Func([]any{int64(9), int64(0), int64(5)})
	require.NoError(t, err)
	require.Equal(t, int64(5), got)

	_, err = clamp.Func([]any{int64(9), "x", int64(5)})
	require.EqualError(t, err, "clamp: argument 2 must be an integer, got a string")

	_, err = clamp.Func([]any{int64(9)})
	require.EqualError(t, err, "clamp: expected 3 arguments, got 1")
}

func TestMakeFuncZeroArgs(t *testing.T) {
	answer := MakeFunc("answer", "d", func() (int64, error) { return 42, nil })
	require.Equal(t, 0, answer.ArgCount)

	got, err := answer.Func([]any{})
	require.NoError(t, err)
	require.Equal(t, int64(42), got)

	_, err = answer.Func([]any{"x"})
	require.EqualError(t, err, "answer: expected 0 arguments, got 1")
}

func TestMakeFuncVariadicForms(t *testing.T) {
	join := MakeFunc("join", "d", func(sep string, parts ...string) (string, error) {
		return strings.Join(parts, sep), nil
	})
	require.Equal(t, 1, join.ArgCount)
	require.True(t, join.Variadic)

	got, err := join.Func([]any{"-", "a", "b", "c"})
	require.NoError(t, err)
	require.Equal(t, "a-b-c", got)

	got, err = join.Func([]any{"-"})
	require.NoError(t, err)
	require.Equal(t, "", got)

	_, err = join.Func([]any{"-", "a", int64(1)})
	require.EqualError(t, err, "join: argument 3 must be a string, got an integer")

	_, err = join.Func([]any{})
	require.EqualError(t, err, "join: expected at least 1 argument, got 0")

	concat := MakeFunc("concat", "d", func(parts ...string) (string, error) {
		return strings.Join(parts, ""), nil
	})
	require.Equal(t, 0, concat.ArgCount)
	require.True(t, concat.Variadic)

	got, err = concat.Func([]any{"a", "b"})
	require.NoError(t, err)
	require.Equal(t, "ab", got)

	got, err = concat.Func([]any{})
	require.NoError(t, err)
	require.Equal(t, "", got)
}

func TestMakeFuncResultConversion(t *testing.T) {
	seq := MakeFunc("seq", "d", func(n int64) ([]int64, error) {
		out := make([]int64, n)
		for i := range n {
			out[i] = i
		}
		return out, nil
	})
	got, err := seq.Func([]any{int64(3)})
	require.NoError(t, err)
	require.Equal(t, []any{int64(0), int64(1), int64(2)}, got)

	tags := MakeFunc("tags", "d", func(s string) (map[string]string, error) {
		return map[string]string{"name": s}, nil
	})
	got, err = tags.Func([]any{"x"})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"name": "x"}, got)
}

func TestMakeFuncImplErrorPassesThrough(t *testing.T) {
	boom := MakeFunc("boom", "d", func(s string) (string, error) {
		return "", fmt.Errorf("boom: %s is no good", s)
	})
	_, err := boom.Func([]any{"x"})
	require.EqualError(t, err, "boom: x is no good")
}

func TestMakeFuncRejectsBadSignatures(t *testing.T) {
	tests := []struct {
		name    string
		fn      any
		wantMsg string
	}{
		{"not a function", 42,
			"function x must be a Go function, got int"},
		{"no results", func(s string) {},
			"function x must return (value, error), got 0 results"},
		{"one result", func(s string) string { return s },
			"function x must return (value, error), got 1 result"},
		{"second result not error", func(s string) (string, string) { return s, s },
			"function x must return (value, error); the second result is string"},
		{"unsupported parameter", func(ch chan int) (bool, error) { return true, nil },
			"function x parameter 1 has unsupported type chan int"},
		{"int parameter is not the currency", func(n int) (bool, error) { return true, nil },
			"function x parameter 1 has unsupported type int"},
		{"unsupported element", func(chs []chan int) (bool, error) { return true, nil },
			"function x parameter 1 has unsupported type []chan int"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.PanicsWithValue(t, tt.wantMsg, func() {
				MakeFunc("x", "d", tt.fn)
			})
		})
	}
}

func TestMakeFuncRejectsBadResults(t *testing.T) {
	type named string
	tests := []struct {
		name    string
		fn      any
		wantMsg string
	}{
		{"named scalar result", func(s string) (named, error) { return named(s), nil },
			"function x result has unsupported type runtime.named"},
		{"chan result", func(s string) (chan int, error) { return nil, nil },
			"function x result has unsupported type chan int"},
		{"named element result", func(s string) ([]named, error) { return nil, nil },
			"function x result has unsupported type []runtime.named"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.PanicsWithValue(t, tt.wantMsg, func() {
				MakeFunc("x", "d", tt.fn)
			})
		})
	}
}
