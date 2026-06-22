package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseComprehension(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		check func(t *testing.T, c *Comprehension)
	}{
		{
			name: "list maps each element",
			in:   "[ for x in input.subnets : x.cidr-block ]",
			check: func(t *testing.T, c *Comprehension) {
				assert.Equal(t, CompList, c.Kind)
				assert.Equal(t, []string{"x"}, c.Names)
				assert.Equal(t, "input", c.Source.(*DotPath).Root.Name)
				assert.Equal(t, "x", c.Value.(*DotPath).Root.Name)
				assert.Nil(t, c.Key)
				assert.Nil(t, c.Filter)
				assert.False(t, c.Group)
			},
		},
		{
			name: "map indexes by a field",
			in:   "{ for x in input.subnets : x.name => x }",
			check: func(t *testing.T, c *Comprehension) {
				assert.Equal(t, CompMap, c.Kind)
				assert.Equal(t, "x", c.Key.(*DotPath).Root.Name)
				assert.Equal(t, "x", c.Value.(*Ident).Name)
				assert.False(t, c.Group)
			},
		},
		{
			name: "list with when filter",
			in:   "[ for x in input.subnets : x.id when x.public ]",
			check: func(t *testing.T, c *Comprehension) {
				assert.Equal(t, CompList, c.Kind)
				require.NotNil(t, c.Filter)
				assert.Equal(t, "x", c.Filter.(*DotPath).Root.Name)
			},
		},
		{
			name: "map group-by with ellipsis",
			in:   "{ for x in input.subnets : x.az => x.id... }",
			check: func(t *testing.T, c *Comprehension) {
				assert.Equal(t, CompMap, c.Kind)
				assert.True(t, c.Group)
				assert.Nil(t, c.Filter)
				assert.Equal(t, "x", c.Value.(*DotPath).Root.Name)
			},
		},
		{
			name: "map group-by with filter",
			in:   "{ for x in xs : x.az => x.id... when x.active }",
			check: func(t *testing.T, c *Comprehension) {
				assert.True(t, c.Group)
				require.NotNil(t, c.Filter)
			},
		},
		{
			name: "two-name list binding",
			in:   "[ for i, x in xs : i ]",
			check: func(t *testing.T, c *Comprehension) {
				assert.Equal(t, []string{"i", "x"}, c.Names)
				assert.Equal(t, "i", c.Value.(*Ident).Name)
			},
		},
		{
			name: "two-name map binding",
			in:   "{ for k, v in m : k => v }",
			check: func(t *testing.T, c *Comprehension) {
				assert.Equal(t, []string{"k", "v"}, c.Names)
				assert.Equal(t, "k", c.Key.(*Ident).Name)
				assert.Equal(t, "v", c.Value.(*Ident).Name)
			},
		},
		{
			name: "bare ident source",
			in:   "[ for x in items : x ]",
			check: func(t *testing.T, c *Comprehension) {
				assert.Equal(t, "items", c.Source.(*Ident).Name)
			},
		},
		{
			name: "conditional body with filter",
			in:   "[ for x in input.subnets : if x.public then x.id else '' when x.active ]",
			check: func(t *testing.T, c *Comprehension) {
				cond := c.Value.(*Conditional)
				assert.Equal(t, "x", cond.Cond.(*DotPath).Root.Name)
				assert.Equal(t, "", cond.Else.(*StringLit).Value)
				require.NotNil(t, c.Filter)
				assert.Equal(t, "x", c.Filter.(*DotPath).Root.Name)
			},
		},
		{
			name: "conditional map value",
			in:   "{ for x in xs : x.k => if x.on then 1 else 0 }",
			check: func(t *testing.T, c *Comprehension) {
				cond := c.Value.(*Conditional)
				assert.Equal(t, int64(1), cond.Then.(*NumberLit).ParsedInt)
				assert.Equal(t, int64(0), cond.Else.(*NumberLit).ParsedInt)
			},
		},
		{
			name: "nested list comprehension",
			in:   "[ for net in input.networks : [ for s in net.subnets : s.id ] ]",
			check: func(t *testing.T, c *Comprehension) {
				inner := c.Value.(*Comprehension)
				assert.Equal(t, []string{"s"}, inner.Names)
				assert.Equal(t, "net", inner.Source.(*DotPath).Root.Name)
				assert.Equal(t, "s", inner.Value.(*DotPath).Root.Name)
			},
		},
		{
			name: "spans multiple lines",
			in:   "[\n  for x in input.subnets :\n  x.id\n  when x.public\n]",
			check: func(t *testing.T, c *Comprehension) {
				assert.Equal(t, "x", c.Value.(*DotPath).Root.Name)
				require.NotNil(t, c.Filter)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseExpr(t, tt.in)
			c, ok := got.(*Comprehension)
			require.True(t, ok, "expected *Comprehension, got %T", got)
			tt.check(t, c)
		})
	}
}

func TestParseComprehensionAsCallArg(t *testing.T) {
	got := parseExpr(t, "length([ for x in xs : x.id ])")
	call := got.(*Call)
	require.Len(t, call.Args, 1)
	assert.IsType(t, (*Comprehension)(nil), call.Args[0])
}

// A `[`/`{` not followed by the `for` keyword stays an ordinary literal,
// and an identifier that merely begins with `for` is not the keyword.
func TestParseComprehensionKeywordGuard(t *testing.T) {
	t.Run("array of one ident", func(t *testing.T) {
		got := parseExpr(t, "[forest]")
		arr := got.(*ArrayLit)
		require.Len(t, arr.Elements, 1)
		assert.Equal(t, "forest", arr.Elements[0].(*Ident).Name)
	})
	t.Run("object with for-prefixed key", func(t *testing.T) {
		got := parseExpr(t, "{ forced: 1 }")
		obj := got.(*ObjectLit)
		require.Len(t, obj.Fields, 1)
		assert.Equal(t, "forced", obj.Fields[0].Key.Name)
	})
	t.Run("plain array still parses", func(t *testing.T) {
		got := parseExpr(t, "[1, 2, 3]")
		assert.Len(t, got.(*ArrayLit).Elements, 3)
	})
}

func TestParseComprehensionErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "list missing body", in: "[ for x in xs ]"},
		{name: "list missing colon", in: "[ for x in xs x.id ]"},
		{name: "map missing value", in: "{ for x in xs : k }"},
		{name: "map missing source", in: "{ for x in : k => v }"},
		{name: "missing in", in: "[ for x xs : x ]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseExpr("", []byte(tt.in))
			require.Error(t, err)
		})
	}
}
