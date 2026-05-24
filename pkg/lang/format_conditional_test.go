package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatConditional(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "inline simple",
			src:  "x: if a then b else c\n",
			want: "x: if a then b else c\n",
		},
		{
			name: "comparison condition",
			src:  "x: if var.n > 3 then 'big' else 'small'\n",
			want: "x: if var.n > 3 then 'big' else 'small'\n",
		},
		{
			name: "multi-line collapses to inline",
			src:  "x: if a\n  then b\n  else c\n",
			want: "x: if a then b else c\n",
		},
		{
			name: "else-if chain stays inline",
			src:  "x: if a then 1 else if b then 2 else 3\n",
			want: "x: if a then 1 else if b then 2 else 3\n",
		},
		{
			name: "messy whitespace normalized",
			src:  "x:   if   a   then   b   else   c\n",
			want: "x: if a then b else c\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, formatString(t, tt.src))
		})
	}
}

func TestFormatConditionalBreaksWhenTooLong(t *testing.T) {
	src := "x: if var.flag then 'aaaaaaaa' else 'bbbbbbbb'\n"
	f, err := ParseSource("test.ub", []byte(src))
	require.NoError(t, err)
	out, err := FormatWith(f, FormatOptions{MaxColumn: 24})
	require.NoError(t, err)
	want := "x: if var.flag\n  then 'aaaaaaaa'\n  else 'bbbbbbbb'\n"
	require.Equal(t, want, string(out))
}

func TestFormatConditionalDeterministic(t *testing.T) {
	src := "x: if var.n > 3 then 'big' else if var.n > 1 then 'mid' else 'small'\n"
	first := formatString(t, src)
	for range 5 {
		require.Equal(t, first, formatString(t, src))
	}
}
