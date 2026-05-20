package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func TestFileCreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")

	f := &File{Path: path, Content: "hi there"}
	out, err := f.Create(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, out)

	require.Equal(t, path, out.Path)
	require.Equal(t, int64(8), out.Size)
	require.Equal(t,
		"9b96a1fe1d548cbbc960cc6a0286668fd74a763667b06366fb2324269fcabaa4",
		out.SHA256)

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "hi there", string(body))
}

func TestFileCreatesMissingParentDirsWhenOptedIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "hello.txt")

	out, err := (&File{Path: path, Content: "deep", CreateDirectory: true}).
		Create(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, path, out.Path)

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "deep", string(body))
}

func TestFileFailsWhenParentMissingAndOptOut(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "hello.txt")

	_, err := (&File{Path: path, Content: "x"}).Create(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no such file or directory")
}

func TestFileWriteWithMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exec.sh")

	_, err := (&File{Path: path, Content: "#!/bin/sh\n", Mode: 0o755}).
		Create(context.Background(), nil)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestFileUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "u.txt")

	f := &File{Path: path, Content: "first"}
	first, err := f.Create(context.Background(), nil)
	require.NoError(t, err)

	f.Content = "second value"
	second, err := f.Update(context.Background(), nil, first)
	require.NoError(t, err)

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "second value", string(body))
	require.NotEqual(t, first.SHA256, second.SHA256)
}

func TestFileReadReportsNotFound(t *testing.T) {
	f := &File{Path: filepath.Join(t.TempDir(), "missing")}
	_, err := f.Read(context.Background(), nil, nil)
	require.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestFileReadFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.txt")
	require.NoError(t, os.WriteFile(path, []byte("on disk"), 0o644))

	f := &File{Path: path}
	out, err := f.Read(context.Background(), nil, nil)
	require.NoError(t, err)
	require.Equal(t, int64(7), out.Size)
}

func TestFileDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.txt")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	require.NoError(t, (&File{Path: path}).Delete(context.Background(), nil, nil))
	_, err := os.Stat(path)
	require.True(t, errors.Is(err, os.ErrNotExist))
}

func TestFileDeleteAbsentIsNoop(t *testing.T) {
	require.NoError(t, (&File{Path: filepath.Join(t.TempDir(), "absent")}).
		Delete(context.Background(), nil, nil))
}

func TestFileRequiresPath(t *testing.T) {
	_, err := (&File{Content: "x"}).Create(context.Background(), nil)
	require.Error(t, err)
}

func TestFileAtomicWriteLeavesNoTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.txt")

	_, err := (&File{Path: path, Content: "data"}).Create(context.Background(), nil)
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.False(t, hasPrefix(e.Name(), "."),
			"atomic write should not leave a hidden tmp file: %s", e.Name())
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func TestFileReplaceFields(t *testing.T) {
	require.Equal(t, []string{"path"}, (&File{}).ReplaceFields())
}

func TestModuleRegistersFile(t *testing.T) {
	mod := Module()
	require.Equal(t, "local", mod.Name)
	rt, ok := mod.Resources["file"]
	require.True(t, ok)
	require.Equal(t, 1, rt.SchemaVersion())
	_, ok = rt.NewReceiver().(*File)
	require.True(t, ok)
}
