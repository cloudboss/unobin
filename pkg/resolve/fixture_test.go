package resolve

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func ubFixtureDir(t testing.TB, name string) string {
	t.Helper()
	root := filepath.Join("testdata/ub", filepath.FromSlash(name))
	require.DirExists(t, root)
	return root
}

func ubFixtureText(t testing.TB, name string) string {
	t.Helper()
	path := filepath.Join("testdata/ub", filepath.FromSlash(name)+".ub")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

func ubFixtureFS(t testing.TB, name string) fstest.MapFS {
	t.Helper()
	root := ubFixtureDir(t, name)
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
	return mfs
}

func newUBFixtureSource(t testing.TB, name string) *Source {
	t.Helper()
	return &Source{FS: ubFixtureFS(t, name), Commit: "ub"}
}
