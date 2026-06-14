package resolve

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func parseSyntaxFile(t *testing.T, path, src string) *syntax.File {
	t.Helper()
	f, err := syntax.ParseSource(path, []byte(src))
	require.NoError(t, err)
	return f
}

func TestExtractSyntaxImportsFactory(t *testing.T) {
	f := parseSyntaxFile(t, "factory.ub", `
factory: {
  imports: {
    aws: 'github.com/cloudboss/unobin-library-aws'
    local: './local-lib'
  }
}
`)

	refs, errs := ExtractSyntaxImports(f)
	require.Empty(t, errs)
	require.Len(t, refs, 2)

	aws := refs[0]
	require.Empty(t, aws.Scope)
	require.Equal(t, "aws", aws.Alias)
	require.Equal(t, "github.com/cloudboss/unobin-library-aws",
		aws.Ref.(*RemoteImport).URL)

	local := refs[1]
	require.Empty(t, local.Scope)
	require.Equal(t, "local", local.Alias)
	require.Equal(t, "./local-lib", local.Ref.(*LocalImport).Path)
}

func TestExtractSyntaxImportsLibraryExports(t *testing.T) {
	f := parseSyntaxFile(t, "library.ub", `
greeting: resource {
  imports: {
    helloer: 'github.com/scratch/repo//ub/helloer'
  }
}

lookup: data {
  imports: {
    local: './local-data'
  }
}
`)

	refs, errs := ExtractSyntaxImports(f)
	require.Empty(t, errs)
	require.Len(t, refs, 2)

	helloer := refs[0]
	require.Equal(t, "resource.greeting", helloer.Scope)
	require.Equal(t, "helloer", helloer.Alias)
	require.Equal(t, "github.com/scratch/repo", helloer.Ref.(*RemoteImport).URL)
	require.Equal(t, "ub/helloer", helloer.Ref.(*RemoteImport).Subdir)

	local := refs[1]
	require.Equal(t, "data.lookup", local.Scope)
	require.Equal(t, "local", local.Alias)
	require.Equal(t, "./local-data", local.Ref.(*LocalImport).Path)
}

func TestExtractSyntaxImportsReportsBadRefs(t *testing.T) {
	f := parseSyntaxFile(t, "factory.ub", `
factory: {
  imports: {
    bad: 'github.com'
  }
}
`)

	refs, errs := ExtractSyntaxImports(f)
	require.Empty(t, refs)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0].Error(), "host and a path")
}
