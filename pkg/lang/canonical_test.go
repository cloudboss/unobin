package lang

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
)

func TestCanonicalizeFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/canonical",
		func(name string, src []byte) (string, []string) {
			out, err := Canonicalize(name+".ub", src)
			if err != nil {
				return "", []string{err.Error()}
			}
			return string(out), nil
		},
		ubtest.Idempotent(),
		ubtest.Repeat(5),
	)
}

func TestWriteCanonical(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "factory.ub")
	draft, err := os.ReadFile("testdata/ub/canonical/valid/blank-lines.ub")
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/ub/canonical/valid/blank-lines.ub.out")
	require.NoError(t, err)

	require.NoError(t, WriteCanonical(path, draft))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "atomic write should leave no temp file behind")
}
