package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSplatSegment(t *testing.T) {
	type seg struct {
		name  string
		splat bool
		index bool
	}
	cases := []struct {
		name string
		in   string
		want []seg
	}{
		{
			name: "splat then field",
			in:   "input.subnets[*].id",
			want: []seg{{name: "subnets"}, {splat: true}, {name: "id"}},
		},
		{
			name: "bare splat",
			in:   "input.list[*]",
			want: []seg{{name: "list"}, {splat: true}},
		},
		{
			name: "index then splat",
			in:   "input.matrix[0][*]",
			want: []seg{{name: "matrix"}, {index: true}, {splat: true}},
		},
		{
			name: "splat then index",
			in:   "input.matrix[*][0]",
			want: []seg{{name: "matrix"}, {splat: true}, {index: true}},
		},
		{
			name: "nested splat then field",
			in:   "input.grid[*][*].name",
			want: []seg{{name: "grid"}, {splat: true}, {splat: true}, {name: "name"}},
		},
		{
			name: "multiple splats then index",
			in:   "input.a[*][*][0]",
			want: []seg{{name: "a"}, {splat: true}, {splat: true}, {index: true}},
		},
		{
			name: "string index then splat",
			in:   "input.m['k'].items[*].id",
			want: []seg{{name: "m"}, {index: true}, {name: "items"}, {splat: true}, {name: "id"}},
		},
		{
			name: "spaces inside brackets",
			in:   "input.list[ * ].id",
			want: []seg{{name: "list"}, {splat: true}, {name: "id"}},
		},
		{
			name: "splat on a node output field",
			in:   "resource.aws.vpc.main.subnets[*].cidr",
			want: []seg{
				{name: "aws"}, {name: "vpc"}, {name: "main"}, {name: "subnets"},
				{splat: true}, {name: "cidr"},
			},
		},
		{
			name: "integer index is not a splat",
			in:   "input.x[0]",
			want: []seg{{name: "x"}, {index: true}},
		},
		{
			name: "string index is not a splat",
			in:   "input.x['k']",
			want: []seg{{name: "x"}, {index: true}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expr, err := ParseExpr("", []byte(c.in))
			require.NoError(t, err)
			dp, ok := expr.(*DotPath)
			require.True(t, ok, "want *DotPath, got %T", expr)
			require.Len(t, dp.Segments, len(c.want))
			for i, w := range c.want {
				s := dp.Segments[i]
				assert.Equal(t, w.name, s.Name, "segment %d name", i)
				assert.Equal(t, w.splat, s.Splat, "segment %d splat", i)
				assert.Equal(t, w.index, s.Index != nil, "segment %d index", i)
			}
		})
	}
}
