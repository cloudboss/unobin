package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatConditionalBreaksWhenTooLong(t *testing.T) {
	src := "x: if var.flag then 'aaaaaaaa' else 'bbbbbbbb'\n"
	f, err := ParseSource("test.ub", []byte(src))
	require.NoError(t, err)
	out, err := FormatWith(f, FormatOptions{MaxColumn: 24})
	require.NoError(t, err)
	want := "x: if var.flag\n  then 'aaaaaaaa'\n  else 'bbbbbbbb'\n"
	require.Equal(t, want, string(out))
}
