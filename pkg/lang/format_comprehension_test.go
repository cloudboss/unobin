package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatComprehension(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "list inline",
			src:  "x: [ for s in var.subnets : s.cidr ]\n",
			want: "x: [ for s in var.subnets : s.cidr ]\n",
		},
		{
			name: "map index-by",
			src:  "x: { for s in var.subnets : s.name => s }\n",
			want: "x: { for s in var.subnets : s.name => s }\n",
		},
		{
			name: "list with filter",
			src:  "x: [ for s in var.subnets : s.id when s.public ]\n",
			want: "x: [ for s in var.subnets : s.id when s.public ]\n",
		},
		{
			name: "map group-by",
			src:  "x: { for s in var.subnets : s.az => s.id... }\n",
			want: "x: { for s in var.subnets : s.az => s.id... }\n",
		},
		{
			name: "map group-by with filter",
			src:  "x: { for s in var.subnets : s.az => s.id... when s.public }\n",
			want: "x: { for s in var.subnets : s.az => s.id... when s.public }\n",
		},
		{
			name: "two-name list binding",
			src:  "x: [ for i, s in var.items : i ]\n",
			want: "x: [ for i, s in var.items : i ]\n",
		},
		{
			name: "two-name map binding",
			src:  "x: { for k, v in var.m : k => v }\n",
			want: "x: { for k, v in var.m : k => v }\n",
		},
		{
			name: "multi-line collapses to inline",
			src:  "x: [\n  for s in var.subnets :\n  s.cidr\n]\n",
			want: "x: [ for s in var.subnets : s.cidr ]\n",
		},
		{
			name: "messy whitespace normalized",
			src:  "x: [for s in var.subnets:s.cidr]\n",
			want: "x: [ for s in var.subnets : s.cidr ]\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, formatString(t, tt.src))
		})
	}
}

func TestFormatComprehensionBreaksWhenTooLong(t *testing.T) {
	src := "x: [ for s in var.subnets : s.identifier when s.public ]\n"
	f, err := ParseSource("test.ub", []byte(src))
	require.NoError(t, err)
	out, err := FormatWith(f, FormatOptions{MaxColumn: 30})
	require.NoError(t, err)
	want := "x: [\n  for s in var.subnets :\n  s.identifier\n  when s.public\n]\n"
	require.Equal(t, want, string(out))
}

func TestFormatComprehensionMapBreaksWhenTooLong(t *testing.T) {
	src := "x: { for s in var.subnets : s.az => s.identifier... }\n"
	f, err := ParseSource("test.ub", []byte(src))
	require.NoError(t, err)
	out, err := FormatWith(f, FormatOptions{MaxColumn: 30})
	require.NoError(t, err)
	want := "x: {\n  for s in var.subnets :\n  s.az => s.identifier...\n}\n"
	require.Equal(t, want, string(out))
}

func TestFormatComprehensionDeterministic(t *testing.T) {
	src := "x: { for s in var.subnets : s.az => s.id... when s.public }\n"
	first := formatString(t, src)
	for i := 0; i < 5; i++ {
		require.Equal(t, first, formatString(t, src))
	}
}
