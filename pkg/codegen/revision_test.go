package codegen

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeLibraryDir lays out a minimal generated-library tree for
// ContentRevision to digest: a main.go, go.mod, go.sum, and one internal
// package file.
func writeLibraryDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module demo\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.sum"),
		[]byte("example.com/x v1.0.0 h1:abc=\n"), 0o644))
	internal := filepath.Join(dir, "internal", "net")
	require.NoError(t, os.MkdirAll(internal, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(internal, "net.go"),
		[]byte("package net\n"), 0o644))
	return dir
}

func TestContentRevisionFormat(t *testing.T) {
	got, err := ContentRevision(writeLibraryDir(t))
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{12}$`), got)
}

func TestContentRevisionDeterministic(t *testing.T) {
	dir := writeLibraryDir(t)
	first, err := ContentRevision(dir)
	require.NoError(t, err)
	for range 5 {
		again, err := ContentRevision(dir)
		require.NoError(t, err)
		require.Equal(t, first, again)
	}
}

func TestContentRevisionChangesWith(t *testing.T) {
	base := writeLibraryDir(t)
	baseSum, err := ContentRevision(base)
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(t *testing.T, dir string)
	}{
		{
			name: "main.go body",
			mutate: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
					[]byte("package main\n\nfunc main() { _ = 1 }\n"), 0o644))
			},
		},
		{
			name: "go.mod body",
			mutate: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
					[]byte("module demo\n\ngo 1.27\n"), 0o644))
			},
		},
		{
			name: "go.sum body",
			mutate: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "go.sum"),
					[]byte("example.com/x v1.0.1 h1:def=\n"), 0o644))
			},
		},
		{
			name: "internal package body",
			mutate: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "net", "net.go"),
					[]byte("package net\n\nvar X int\n"), 0o644))
			},
		},
		{
			name: "new source file",
			mutate: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.go"),
					[]byte("package main\n"), 0o644))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeLibraryDir(t)
			tt.mutate(t, dir)
			got, err := ContentRevision(dir)
			require.NoError(t, err)
			require.NotEqual(t, baseSum, got)
		})
	}
}

func TestContentRevisionIgnoresNonSource(t *testing.T) {
	dir := writeLibraryDir(t)
	want, err := ContentRevision(dir)
	require.NoError(t, err)

	// The compiled binary and other non-source files must not move the hash.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "demo"),
		[]byte("\x7fELF binary bytes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"),
		[]byte("notes\n"), 0o644))

	got, err := ContentRevision(dir)
	require.NoError(t, err)
	require.Equal(t, want, got)
}
