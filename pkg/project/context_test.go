package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/libraryapi"
)

func TestCompatibilityUsesInjectedDescriptor(t *testing.T) {
	descriptor := libraryapi.Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.1"}, GeneratorAPI: "1.1",
	}
	options := Options{UnobinVersion: "v0.1.0", LibraryAPIDescriptor: &descriptor}
	context, err := options.Compatibility(t.TempDir(), nil, "")
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS("../deps/testdata/go/compatibility")))
	path := filepath.Join(root, "library.go")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	source = []byte(strings.ReplaceAll(string(source), `RequiredAPI: "1.0"`, `RequiredAPI: "1.1"`))
	require.NoError(t, os.WriteFile(path, source, 0o644))
	require.NoError(t, context.CheckDirectory(root, true))
}
