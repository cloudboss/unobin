package resolve

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func TestExtractSyntaxImportsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/syntax-imports",
		func(name string, src []byte) (string, []string) {
			f, err := syntax.ParseSource(syntaxImportFixturePath(name), src)
			require.NoError(t, err)

			refs, errs := ExtractSyntaxImports(f)
			output := syntaxImportOutput(refs)
			diags := make([]string, 0, len(errs))
			for _, err := range errs {
				diags = append(diags, err.Error())
			}
			return output, diags
		})
}

func syntaxImportFixturePath(name string) string {
	base := filepath.Base(name)
	if strings.Contains(base, "library") {
		return "library.ub"
	}
	return "factory.ub"
}

func syntaxImportOutput(refs []SyntaxImport) string {
	if len(refs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, ref := range refs {
		scope := ref.Scope
		if scope == "" {
			scope = "-"
		}
		fmt.Fprintf(&b, "%s %s %s\n", scope, ref.Alias, syntaxImportRef(ref.Ref))
	}
	return b.String()
}

func syntaxImportRef(ref ImportRef) string {
	switch r := ref.(type) {
	case *RemoteImport:
		if r.Subdir != "" {
			return fmt.Sprintf("remote %s//%s", r.URL, r.Subdir)
		}
		return fmt.Sprintf("remote %s", r.URL)
	case *LocalImport:
		return fmt.Sprintf("local %s", r.Path)
	default:
		return fmt.Sprintf("%T", ref)
	}
}
