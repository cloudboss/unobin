package sourcecheck

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestImportAnalysisRejectsUBChangesDuringSchemaRead(t *testing.T) {
	for _, relative := range []string{"factory.ub", "level-1/library.ub"} {
		t.Run(relative, func(t *testing.T) {
			fixture := newAnalysisBenchmark(t, "valid/analysis-scaling/nested/depth-2/factory")
			path := filepath.Join(fixture.options.Source.Path, relative)
			fixture.options.SchemaCache = NewSchemaCacheWithReader(
				func(string) (*runtime.LibrarySchema, []string, error) {
					require.NoError(t, os.WriteFile(path, nil, 0o644))
					return importAnalysisSchema(), nil, nil
				})
			analysis, err := AnalyzeImports(fixture.refs, fixture.options)
			require.Nil(t, analysis)
			require.ErrorContains(t, err, "source changed")
		})
	}
}
