package resolve

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func newUBFixtureSource(t testing.TB, name string) *Source {
	t.Helper()
	root := filepath.Join("testdata/ub", filepath.FromSlash(name))
	require.DirExists(t, root)
	mfs := fstest.MapFS{}
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		mfs[filepath.ToSlash(rel)] = &fstest.MapFile{Data: body}
		return nil
	}))
	return &Source{FS: mfs, Commit: "ub"}
}
