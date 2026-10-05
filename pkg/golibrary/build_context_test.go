package golibrary

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLibraryReadersExcludeGeneratorsAndCheckDeclarations(t *testing.T) {
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "amd64")
	t.Setenv("CGO_ENABLED", "0")
	t.Setenv("GOFLAGS", "")
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS(
		filepath.Join("..", "gopackage", "testdata", "buildcontext"))))
	declaration, err := ReadCompatibility(dir, dir)
	require.NoError(t, err)
	require.Equal(t, "1.0", declaration.RequiredAPI)
	require.Equal(t, "library.go", filepath.Base(declaration.Path))
	validation, err := ValidatePackage(dir, dir)
	require.NoError(t, err)
	require.Equal(t, "buildcontext", validation.PackageName)
	require.True(t, validation.HasData)
	for _, name := range []string{"z_alternative.go", "_alternative.go", ".alternative.go"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			body := []byte("//go:build special\n\n" + librarySource(runtimePackage("other"),
				`func Library() *other.Library {
	return &other.Library{Compatibility: other.LibraryCompatibility{RequiredAPI: "2.0"}}
}`))
			require.NoError(t, os.WriteFile(path, body, 0o644))
			t.Cleanup(func() { require.NoError(t, os.Remove(path)) })
			_, err := ReadCompatibility(dir, dir)
			require.ErrorContains(t, err, "more than one package-level library function")
			_, err = ValidatePackage(dir, dir)
			require.ErrorContains(t, err, "more than one package-level library function")
		})
	}
}

func TestSourceSnapshotIncludesTargetAndInactiveDeclarations(t *testing.T) {
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "amd64")
	t.Setenv("CGO_ENABLED", "0")
	t.Setenv("GOFLAGS", "")
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS(
		filepath.Join("..", "gopackage", "testdata", "buildcontext"))))
	before, err := SourceSnapshot(dir, nil)
	require.NoError(t, err)
	t.Setenv("GOOS", "windows")
	after, err := SourceSnapshot(dir, nil)
	require.NoError(t, err)
	require.NotEqual(t, before, after)
	t.Setenv("GOOS", "linux")
	unchanged, err := SourceSnapshot(dir, nil)
	require.NoError(t, err)
	require.Equal(t, before, unchanged)
	path := filepath.Join(dir, "a_generate.go")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(body, []byte("\nconst Changed = true\n")...),
		0o644))
	changed, err := SourceSnapshot(dir, nil)
	require.NoError(t, err)
	require.NotEqual(t, before, changed)
}
