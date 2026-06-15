package lang

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalize(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "already canonical is unchanged",
			src:  "a: 'x'\nb: 'y'\n",
			want: "a: 'x'\nb: 'y'\n",
		},
		{
			name: "collapses extra blank lines and trims trailing space",
			src:  "a: 'x'   \n\n\n\nb: 'y'\n",
			want: "a: 'x'\n\nb: 'y'\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Canonicalize("t.ub", []byte(tt.src))
			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))

			again, err := Canonicalize("t.ub", got)
			require.NoError(t, err)
			require.Equal(t, string(got), string(again), "Canonicalize is not idempotent")
		})
	}
}

// TestCanonicalizeWrapsByDefault proves wrap-strings is on: a string past
// the line budget folds rather than staying single-quoted. The exact fold
// geometry is covered by the formatter tests.
func TestCanonicalizeWrapsByDefault(t *testing.T) {
	long := "url: 'https://example.com/registry/cloudboss/unobin-library-aws/path/that/" +
		"runs/well/past/the/hundred/column/budget'\n"
	got, err := Canonicalize("t.ub", []byte(long))
	require.NoError(t, err)
	require.Contains(t, string(got), "'''\\-", "long value should wrap to joined triple form")
}

// TestCanonicalizeWrapPreservesValue proves that wrapping a long string
// never changes its content: the joined triple-quote form re-parses to
// the original value. A change here is a formatter bug, not a reason to
// avoid wrapping.
func TestCanonicalizeWrapPreservesValue(t *testing.T) {
	values := []string{
		"https://example.com/registry/cloudboss/unobin-library-aws/path/that/overflows/the/line/budget",
		"a fairly long human sentence with spaces that runs comfortably past the hundred column budget here",
	}
	for _, want := range values {
		t.Run(want[:16], func(t *testing.T) {
			out, err := Canonicalize("t.ub", []byte("v: '"+want+"'\n"))
			require.NoError(t, err)

			f, err := ParseSource("t.ub", out)
			require.NoError(t, err)
			lit, ok := f.Body.Fields[0].Value.(*StringLit)
			require.True(t, ok, "value should re-parse as a string literal")
			require.Equal(t, want, lit.Value)
		})
	}
}

func TestCanonicalizeParseError(t *testing.T) {
	_, err := Canonicalize("t.ub", []byte("a: {\n"))
	require.Error(t, err)
}

func TestWriteCanonical(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "factory.ub")
	draft := []byte("a: 'x'   \n\n\n\nb: 'y'\n")

	require.NoError(t, WriteCanonical(path, draft))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	want, err := Canonicalize("factory.ub", draft)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "atomic write should leave no temp file behind")
}
