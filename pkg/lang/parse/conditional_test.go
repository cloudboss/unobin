package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseExpr(t *testing.T, in string) Expr {
	t.Helper()
	e, err := ParseExpr("", []byte(in))
	require.NoError(t, err)
	return e
}

func TestParseConditional(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		check func(t *testing.T, c *Conditional)
	}{
		{
			name: "bare idents",
			in:   "if a then b else c",
			check: func(t *testing.T, c *Conditional) {
				assert.Equal(t, "a", c.Cond.(*Ident).Name)
				assert.Equal(t, "b", c.Then.(*Ident).Name)
				assert.Equal(t, "c", c.Else.(*Ident).Name)
			},
		},
		{
			name: "dot-path condition",
			in:   "if x.ready then 1 else 0",
			check: func(t *testing.T, c *Conditional) {
				assert.Equal(t, "x", c.Cond.(*DotPath).Root.Name)
				assert.Equal(t, int64(1), c.Then.(*NumberLit).ParsedInt)
				assert.Equal(t, int64(0), c.Else.(*NumberLit).ParsedInt)
			},
		},
		{
			name: "comparison condition",
			in:   "if x.count > 3 then 'big' else 'small'",
			check: func(t *testing.T, c *Conditional) {
				infix := c.Cond.(*Infix)
				assert.Equal(t, ">", infix.Op)
				assert.Equal(t, "big", c.Then.(*StringLit).Value)
				assert.Equal(t, "small", c.Else.(*StringLit).Value)
			},
		},
		{
			name: "else-if chains right",
			in:   "if a then 1 else if b then 2 else 3",
			check: func(t *testing.T, c *Conditional) {
				assert.Equal(t, "a", c.Cond.(*Ident).Name)
				assert.Equal(t, int64(1), c.Then.(*NumberLit).ParsedInt)
				inner := c.Else.(*Conditional)
				assert.Equal(t, "b", inner.Cond.(*Ident).Name)
				assert.Equal(t, int64(2), inner.Then.(*NumberLit).ParsedInt)
				assert.Equal(t, int64(3), inner.Else.(*NumberLit).ParsedInt)
			},
		},
		{
			name: "nested then branch",
			in:   "if a then if b then 1 else 2 else 3",
			check: func(t *testing.T, c *Conditional) {
				inner := c.Then.(*Conditional)
				assert.Equal(t, "b", inner.Cond.(*Ident).Name)
				assert.Equal(t, int64(1), inner.Then.(*NumberLit).ParsedInt)
				assert.Equal(t, int64(2), inner.Else.(*NumberLit).ParsedInt)
				assert.Equal(t, int64(3), c.Else.(*NumberLit).ParsedInt)
			},
		},
		{
			name: "parenthesized conditional condition",
			in:   "if (if a then b else c) then x else y",
			check: func(t *testing.T, c *Conditional) {
				inner := c.Cond.(*Conditional)
				assert.Equal(t, "a", inner.Cond.(*Ident).Name)
				assert.Equal(t, "x", c.Then.(*Ident).Name)
				assert.Equal(t, "y", c.Else.(*Ident).Name)
			},
		},
		{
			name: "spans multiple lines",
			in:   "if a\n  then b\n  else c",
			check: func(t *testing.T, c *Conditional) {
				assert.Equal(t, "a", c.Cond.(*Ident).Name)
				assert.Equal(t, "b", c.Then.(*Ident).Name)
				assert.Equal(t, "c", c.Else.(*Ident).Name)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseExpr(t, tt.in)
			c, ok := got.(*Conditional)
			require.True(t, ok, "expected *Conditional, got %T", got)
			tt.check(t, c)
		})
	}
}

func TestParseConditionalPlacement(t *testing.T) {
	t.Run("call argument", func(t *testing.T) {
		got := parseExpr(t, "format('%s', if p then 'a' else 'b')")
		call := got.(*Call)
		require.Len(t, call.Args, 2)
		assert.IsType(t, (*Conditional)(nil), call.Args[1])
	})
	t.Run("array element", func(t *testing.T) {
		got := parseExpr(t, "[if p then 1 else 2, 3]")
		arr := got.(*ArrayLit)
		require.Len(t, arr.Elements, 2)
		assert.IsType(t, (*Conditional)(nil), arr.Elements[0])
	})
	t.Run("object value", func(t *testing.T) {
		got := parseExpr(t, "{ n: if p then 1 else 2 }")
		obj := got.(*ObjectLit)
		require.Len(t, obj.Fields, 1)
		assert.IsType(t, (*Conditional)(nil), obj.Fields[0].Value)
	})
	t.Run("parenthesized operand", func(t *testing.T) {
		got := parseExpr(t, "1 + (if p then 2 else 3)")
		infix := got.(*Infix)
		assert.Equal(t, "+", infix.Op)
		assert.IsType(t, (*Conditional)(nil), infix.Right)
	})
}

func TestParseConditionalKeywordGuard(t *testing.T) {
	// A kebab identifier that merely starts with `if` is not the keyword.
	got := parseExpr(t, "if-enabled")
	assert.Equal(t, "if-enabled", got.(*Ident).Name)
}

func TestParseConditionalErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "missing then", in: "if a b else c"},
		{name: "missing else", in: "if a then b"},
		{name: "missing else branch", in: "if a then b else"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseExpr("", []byte(tt.in))
			require.Error(t, err)
		})
	}
}
