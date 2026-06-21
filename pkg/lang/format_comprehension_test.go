package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
