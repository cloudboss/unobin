package resolve

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func parseStack(t *testing.T, src string) *lang.File {
	t.Helper()
	f, err := lang.ParseSource("stack.ub", []byte(src))
	require.NoError(t, err)
	return f
}

func TestExtractImportsHappy(t *testing.T) {
	f := parseStack(t, `
imports: {
  aws:   'github.com/x/y@v1.0.0'
  net:   'github.com/x/y//net@v1.0.0'
  local: './local-lib'
}
`)
	refs, errs := ExtractImports(f)
	require.Empty(t, errs)
	require.Len(t, refs, 3)

	aws := refs["aws"].(*RemoteImport)
	require.Equal(t, "github.com/x/y", aws.URL)
	require.Equal(t, "", aws.Subdir)
	require.Equal(t, "v1.0.0", aws.Version)

	net := refs["net"].(*RemoteImport)
	require.Equal(t, "net", net.Subdir)

	local := refs["local"].(*LocalImport)
	require.Equal(t, "./local-lib", local.Path)
}

func TestExtractImportsAbsentBlock(t *testing.T) {
	f := parseStack(t, `description: 'no imports'`)
	refs, errs := ExtractImports(f)
	require.Nil(t, refs)
	require.Empty(t, errs)
}

func TestExtractImportsRejectsBadRef(t *testing.T) {
	f := parseStack(t, `
imports: {
  bad: 'github.com/x/y'
}
`)
	refs, errs := ExtractImports(f)
	require.Empty(t, refs)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0].Error(), "missing required `@version`")
}

func TestExtractImportsSkipsShapeErrors(t *testing.T) {
	// Non-string values and meta keys are reported by lang.ValidateImports,
	// not by ExtractImports.
	f := parseStack(t, `
imports: {
  ok:    'github.com/x/y@v1.0.0'
  bad:   42
  @bogus: 'github.com/x/z@v1.0.0'
}
`)
	refs, errs := ExtractImports(f)
	require.Empty(t, errs)
	require.Len(t, refs, 1)
	require.Contains(t, refs, "ok")
}

func TestResolveImportsLocal(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "libraries", "net"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "libraries", "net", "library.ub"),
		[]byte("description: 'net'\n"), 0o644))

	f := parseStack(t, `
imports: {
  net: './libraries/net'
}
`)
	resolved, errs := ResolveImports(f, NewLocalResolver(root))
	require.Empty(t, errs)
	require.Len(t, resolved, 1)

	got := resolved["net"]
	require.NotNil(t, got)
	require.NotNil(t, got.Source)
	require.True(t, IsUBLibrary(got.Source))
}

func TestResolveImportsPropagatesResolverErrors(t *testing.T) {
	f := parseStack(t, `
imports: {
  aws: 'github.com/x/y@v1.0.0'
}
`)
	boom := errors.New("resolver said no")
	resolved, errs := ResolveImports(f, stubResolver{err: boom})
	require.Len(t, errs, 1)
	require.True(t, errors.Is(errs[0], boom))

	got := resolved["aws"]
	require.NotNil(t, got)
	require.Nil(t, got.Source)
	require.NotNil(t, got.Ref)
}

func TestResolveImportsCollectsVersionConflicts(t *testing.T) {
	f := parseStack(t, `
imports: {
  a: 'github.com/x/y//a@v1.0.0'
  b: 'github.com/x/y//b@v1.1.0'
}
`)
	_, errs := ResolveImports(f, stubResolver{err: errors.New("ignored")})
	conflict := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "same repo") {
			conflict = true
			break
		}
	}
	require.True(t, conflict, "got: %v", errs)
}
