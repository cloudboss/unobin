package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAtomicWriteCreates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	require.NoError(t, WriteFileAtomic(path, []byte("hello"), 0o644))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestAtomicWriteOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	require.NoError(t, WriteFileAtomic(path, []byte("first"), 0o644))
	require.NoError(t, WriteFileAtomic(path, []byte("second"), 0o644))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "second", string(got))
}

func TestAtomicWriteHonorsMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.sh")
	require.NoError(t, WriteFileAtomic(path, []byte("#!/bin/sh\n"), 0o755))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestAtomicWriteLeavesNoTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	require.NoError(t, WriteFileAtomic(path, []byte("data"), 0o644))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.False(t, strings.HasPrefix(e.Name(), "."),
			"hidden tmp file left behind: %s", e.Name())
	}
}

func TestAtomicWriteRejectsBadPath(t *testing.T) {
	require.Error(t, WriteFileAtomic("", []byte("x"), 0o644))
	require.Error(t, WriteFileAtomic(".", []byte("x"), 0o644))
	require.Error(t, WriteFileAtomic("..", []byte("x"), 0o644))
}

func TestAtomicWriteFailsOnMissingDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "x.txt")
	require.Error(t, WriteFileAtomic(path, []byte("x"), 0o644))
}
