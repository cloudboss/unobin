package codegen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
)

func TestWriteSourceRejectsEditsBeforePublication(t *testing.T) {
	for _, conflict := range []string{"modified source", "invalid module requirements"} {
		t.Run(conflict, func(t *testing.T) {
			dir := t.TempDir()
			body := ubtest.ReadValidFixture(t, "testdata/ub/write-source", "minimal")
			input := testMainInput(t, body, "demo")
			_, err := writeSource(t, dir, input)
			require.NoError(t, err)
			main := filepath.Join(dir, "main.go")
			if conflict == "modified source" {
				require.NoError(t, os.WriteFile(main, []byte("authored changes"), 0o644))
			} else {
				input.FactoryName = "changed"
				input.GoImports = map[string]string{"missing": "example.com/missing"}
			}
			before, err := os.ReadFile(main)
			require.NoError(t, err)
			changes, err := writeSource(t, dir, input)
			require.Error(t, err)
			require.Empty(t, changes)
			after, err := os.ReadFile(main)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
