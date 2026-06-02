package resolve

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func parseStack(t *testing.T, src string) *lang.File {
	t.Helper()
	f, err := lang.ParseSource("main.ub", []byte(src))
	require.NoError(t, err)
	return f
}

func TestExtractImportsHappy(t *testing.T) {
	f := parseStack(t, `
imports: {
  aws:   'github.com/x/y'
  net:   'github.com/x/y//net'
  local: './local-lib'
}
`)
	refs, errs := ExtractImports(f)
	require.Empty(t, errs)
	require.Len(t, refs, 3)

	aws := refs["aws"].(*RemoteImport)
	require.Equal(t, "github.com/x/y", aws.URL)
	require.Equal(t, "", aws.Subdir)
	require.Empty(t, aws.Version)

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
  bad: 'github.com'
}
`)
	refs, errs := ExtractImports(f)
	require.Empty(t, refs)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0].Error(), "host and a path")
}

func TestExtractImportsSkipsShapeErrors(t *testing.T) {
	// Non-string values and meta keys are reported by lang.ValidateImports,
	// not by ExtractImports.
	f := parseStack(t, `
imports: {
  ok:    'github.com/x/y'
  bad:   42
  @bogus: 'github.com/x/z'
}
`)
	refs, errs := ExtractImports(f)
	require.Empty(t, errs)
	require.Len(t, refs, 1)
	require.Contains(t, refs, "ok")
}
